package bash_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
)

func TestMain(m *testing.M) {
	sandbox.Init()
	os.Exit(m.Run())
}

func testRoot(t *testing.T) (*file.Root, string) {
	t.Helper()

	if err := sandbox.Available(); err != nil {
		t.Skipf("landlock is unavailable: %v", err)
	}

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll), directory
}

func fixedShell(root *file.Root, policy func() sandbox.Policy) tool.Tool {
	return bash.New(
		root,
		func(context.Context) (sandbox.Policy, error) { return policy(), nil },
		func(context.Context, string) error { return nil },
		sandbox.Direct(),
		true,
	)
}

type recordingRunner struct {
	policy   sandbox.Policy
	runCount int
}

func (self *recordingRunner) Run(
	_ context.Context,
	_ string,
	_ string,
	policy sandbox.Policy,
) (sandbox.Result, error) {
	self.policy = policy
	self.runCount++
	return sandbox.Result{}, nil
}

func (self *recordingRunner) Start(
	_ context.Context,
	_ string,
	_ string,
	_ sandbox.Policy,
	_ sandbox.Output,
) (sandbox.Command, error) {
	panic("unexpected Start call")
}

func TestTheToolIsCalledExec(t *testing.T) {
	root, directory := testRoot(t)

	if name := fixedShell(root, func() sandbox.Policy { return sandbox.Policy{Write: []string{directory}} }).Name(); name != "bash" {
		t.Errorf("got %q, want %q", name, "bash")
	}
}

func TestNetworkingFollowsTheArgument(t *testing.T) {
	root, _ := testRoot(t)

	for _, test := range []struct {
		name      string
		arguments string
		want      bool
	}{
		{name: "omitted", arguments: `{"command":"true"}`},
		{name: "loopback", arguments: `{"command":"true","network":"loopback"}`},
		{name: "host", arguments: `{"command":"true","network":"host"}`, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			approvalCount := 0
			shell := bash.New(
				root,
				func(context.Context) (sandbox.Policy, error) {
					return sandbox.Policy{Network: true}, nil
				},
				func(_ context.Context, command string) error {
					approvalCount++
					if command != "true" {
						t.Errorf("got approval for %q, want %q", command, "true")
					}
					return nil
				},
				runner,
				true,
			)

			call, err := shell.Parse(test.arguments)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, err := call.Exec(t.Context()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runner.policy.Network != test.want {
				t.Errorf("got networking %t, want %t", runner.policy.Network, test.want)
			}
			wantApprovalCount := 0
			if test.want {
				wantApprovalCount = 1
			}
			if approvalCount != wantApprovalCount {
				t.Errorf("got %d approvals, want %d", approvalCount, wantApprovalCount)
			}
		})
	}
}

func TestANetworkNobodyOffersIsRefused(t *testing.T) {
	root, _ := testRoot(t)
	shell := bash.New(
		root,
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		func(context.Context, string) error { return nil },
		&recordingRunner{},
		true,
	)

	if _, err := shell.Parse(`{"command":"true","network":"elsewhere"}`); err == nil {
		t.Fatal("a network nobody offers was accepted")
	}
}

func TestDeniedNetworkingDoesNotRun(t *testing.T) {
	root, _ := testRoot(t)
	runner := &recordingRunner{}
	approvalFailure := errors.New("no")
	shell := bash.New(
		root,
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		func(context.Context, string) error { return approvalFailure },
		runner,
		true,
	)

	call, err := shell.Parse(`{"command":"true","network":"host"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := call.Exec(t.Context()); !errors.Is(err, approvalFailure) {
		t.Fatalf("got %v, want the approval failure", err)
	}
	if runner.runCount != 0 {
		t.Errorf("the runner was called %d times", runner.runCount)
	}
}

func TestAnEmptyCommandIsRefusedDuringParsing(t *testing.T) {
	call, err := fixedShell(nil, func() sandbox.Policy { return sandbox.Policy{} }).
		Parse(`{"command": "   "}`)

	if err == nil || err.Error() != "command is required" {
		t.Fatalf("expected the required-command error, got %v", err)
	}
	if call != nil {
		t.Errorf("expected no call, got %T", call)
	}
}

func TestInvalidBashIsRefusedDuringParsing(t *testing.T) {
	call, err := fixedShell(nil, func() sandbox.Policy { return sandbox.Policy{} }).
		Parse(`{"command": "if true; then"}`)

	if err == nil || !strings.Contains(err.Error(), "invalid Bash command") {
		t.Fatalf("expected an invalid Bash error, got %v", err)
	}
	if call != nil {
		t.Errorf("expected no call, got %T", call)
	}
}

func TestACommandIsRenderedOnOneLine(t *testing.T) {
	for name, command := range map[string]string{
		"newline":        "echo one\necho two",
		"carriage":       "echo one\recho two",
		"tab":            "echo\tone",
		"windows":        "echo one\r\necho two",
		"trailing":       "echo one\n",
		"blank between":  "echo one\n\n\necho two",
		"leading indent": "  echo one\n    echo two",
	} {
		subject, _ := bash.Describe(bash.Args{Command: command})

		if strings.ContainsAny(subject, "\n\r\t") {
			t.Errorf("%s: expected one line, got %q", name, subject)
		}
	}
}

func TestACommandRenderingIsMarkedAsBash(t *testing.T) {
	root, _ := testRoot(t)
	call, err := fixedShell(root, func() sandbox.Policy { return sandbox.Policy{} }).Parse(`{"command":"echo one"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if call.Emphasis() != want {
		t.Errorf("expected bash emphasis, got %T", call)
	}
}

func TestTheSharedCommandRenderingMatchesTheBashTool(t *testing.T) {
	command := "echo one\necho two"
	rendering := bash.DescribeCommand(command)

	parsedCall, err := fixedShell(nil, func() sandbox.Policy { return sandbox.Policy{} }).Parse(
		`{"command":"echo one\necho two"}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	if rendering.Name != "bash" || rendering.Subject != parsedCall.Subject() ||
		rendering.Qualifier != parsedCall.Qualifier() || rendering.Emphasis != parsedCall.Emphasis() {
		t.Errorf("got %#v, want the bash tool's complete rendering", rendering)
	}
}

func TestCommandsAreFormattedOnOneLine(t *testing.T) {
	for name, test := range map[string]struct {
		command string
		want    string
	}{
		"sequential":           {"echo one\necho two", "echo one; echo two"},
		"explicit separator":   {"echo one;\necho two", "echo one; echo two"},
		"conditional pipeline": {"echo one &&\necho two", "echo one && echo two"},
		"pipeline":             {"echo one |\ngrep one", "echo one | grep one"},
		"if":                   {"if true; then\necho one\nfi", "if true; then echo one; fi"},
		"loop":                 {"for item in one two; do\necho $item\ndone", "for item in one two; do echo $item; done"},
		"group":                {"{\necho one\n}", "{ echo one; }"},
		"subshell":             {"(\necho one\n)", "(echo one)"},
		"command substitution": {"echo $(\nprintf one\n)", "echo $(printf one)"},
		"case then subshell":   {"NAME=alpha\ncase \"$NAME\" in alpha) echo alpha ;; esac\n(echo beta)", "NAME=alpha; case \"$NAME\" in alpha) echo alpha ;; esac; (echo beta)"},
		"assignment":           {"GOCACHE=/tmp/io-go-cache go   list", "GOCACHE=/tmp/io-go-cache go list"},
		"blank lines":          {"echo one\n\n\necho two", "echo one; echo two"},
	} {
		subject, _ := bash.Describe(bash.Args{Command: test.command})
		if subject != test.want {
			t.Errorf("%s: got %q, want %q", name, subject, test.want)
		}
	}
}

func TestCommentsAreOmittedFromTheRenderedSummary(t *testing.T) {
	command := "echo one # not displayed\necho two"
	subject, _ := bash.Describe(bash.Args{Command: command})

	if subject != "echo one; echo two" {
		t.Errorf("got %q, want comments omitted", subject)
	}
	if !strings.Contains(command, "# not displayed") {
		t.Error("expected the original command to remain unchanged")
	}
}

func TestAHereDocumentIsShownByItsOpeningLineAlone(t *testing.T) {
	subject, qualifier := bash.Describe(bash.Args{Command: "cat <<'EOF'\none\nEOF"})

	if strings.ContainsAny(subject, "\n\r\t") {
		t.Errorf("expected one line, got %q", subject)
	}
	if subject != "cat <<'EOF'" {
		t.Errorf("got %q, want the opening line without the body joined onto it", subject)
	}
	if qualifier != "3L" {
		t.Errorf("got %q, want the original line count", qualifier)
	}
}

func TestAHereDocumentKeepsItsCompleteEmphasisSource(t *testing.T) {
	root, _ := testRoot(t)
	call, err := fixedShell(root, func() sandbox.Policy { return sandbox.Policy{} }).Parse(
		`{"command":"cat <<EOF\none\nEOF"}`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := call.Emphasis().Source, "cat <<EOF\none\nEOF"; got != want {
		t.Errorf("got source %q, want %q", got, want)
	}
}

func TestACommandOverSeveralLinesSaysHowMany(t *testing.T) {
	for command, want := range map[string]string{
		"echo one":                  "",
		"echo one\n":                "",
		"echo one\necho two":        "2L",
		"echo one\necho two\nls -l": "3L",
	} {
		if _, qualifier := bash.Describe(bash.Args{Command: command}); qualifier != want {
			t.Errorf("%q: expected %q, got %q", command, want, qualifier)
		}
	}
}

func repository(t *testing.T, directory string) string {
	t.Helper()

	metadata := filepath.Join(directory, ".git")

	if err := os.Mkdir(metadata, 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(metadata, "HEAD"), []byte("intact"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return metadata
}

func TestARepositoryIsReadableRatherThanWritable(t *testing.T) {
	_, directory := testRoot(t)
	metadata := repository(t, directory)

	policy := bash.ProtectedPolicy(sandbox.Policy{Write: []string{directory}})

	if !slices.Contains(policy.Read, metadata) {
		t.Errorf("got %v, want it to hold %s back", policy.Read, metadata)
	}

	if slices.Contains(policy.Write, metadata) {
		t.Errorf("got %v, want no write of %s", policy.Write, metadata)
	}
}

func TestAWorkspaceWithoutARepositoryIsLeftAsItIs(t *testing.T) {
	_, directory := testRoot(t)

	if policy := bash.ProtectedPolicy(sandbox.Policy{Write: []string{directory}}); len(policy.Read) > 0 {
		t.Errorf("got %v, want nothing held back", policy.Read)
	}
}

func TestTheToolIsNeverConcurrent(t *testing.T) {
	root, directory := testRoot(t)

	if fixedShell(root, func() sandbox.Policy { return sandbox.Policy{Write: []string{directory}} }).Concurrent() {
		t.Errorf("a shell command is not safe to run alongside others")
	}
}

func TestTheToolAlwaysSaysItMayChangeSomething(t *testing.T) {
	root, directory := testRoot(t)

	shell := fixedShell(root, func() sandbox.Policy {
		return sandbox.Policy{Write: []string{directory}}
	})

	if shell.ReadOnly() {
		t.Errorf("a shell may change something whatever the policy of the moment grants")
	}
}

func allowAll(string) error { return nil }

func TestAChainIsSplitIntoTheStepsItRuns(t *testing.T) {
	for name, test := range map[string]struct {
		command string
		want    []string
	}{
		"one command": {
			command: "curl example.com",
			want:    []string{"curl example.com"},
		},
		"a chain": {
			command: "cd /tmp && curl -sS example.com | jq .name && echo done || echo failed",
			want: []string{
				"cd /tmp &&",
				"curl -sS example.com | jq .name &&",
				"echo done ||",
				"echo failed",
			},
		},
		"a loop keeps its body": {
			command: "for f in *.go; do gofmt -w $f && echo $f; done",
			want:    []string{"for f in *.go; do gofmt -w $f && echo $f; done"},
		},
		"several statements": {
			command: "echo one; echo two",
			want:    []string{"echo one; echo two"},
		},
		"a heredoc keeps its body": {
			command: "cat <<EOF > /tmp/note.txt\n  body\nEOF",
			want:    []string{"cat <<EOF > /tmp/note.txt\n  body\nEOF"},
		},
		"a chain ending in a heredoc keeps every word": {
			command: "cd /tmp && cat <<EOF > note.txt\nbody\nEOF",
			want:    []string{"cd /tmp && cat <<EOF > note.txt\nbody\nEOF"},
		},
		"nothing that parses": {
			command: "curl example.com && ",
			want:    []string{"curl example.com && "},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := bash.Steps(test.command)
			if !slices.Equal(got, test.want) {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestACommandWhoseMeaningWouldChangeIsLeftWhole(t *testing.T) {
	for name, command := range map[string]string{
		"a word ending in a continuation": "\\0$0\\\n",
		"a heredoc of its own":            "cat <<EOF > note\nbody\nEOF",
	} {
		t.Run(name, func(t *testing.T) {
			if got := bash.Steps(command); !slices.Equal(got, []string{command}) {
				t.Errorf("got %q, want the command left whole", got)
			}
		})
	}
}

func TestAnUnconfinedShellOffersNoNetworkChoice(t *testing.T) {
	root, _ := testRoot(t)
	runner := &recordingRunner{}
	shell := bash.New(
		root,
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{Yolo: true}, nil },
		func(context.Context, string) error {
			t.Error("an unconfined shell asked for approval")
			return nil
		},
		runner,
		false,
	)

	for _, parameter := range shell.Schema() {
		if parameter.Name == "network" {
			t.Error("an unconfined shell offers a network parameter")
		}
	}

	call, err := shell.Parse(`{"command":"true","network":"host"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.policy.Network {
		t.Error("an unconfined shell took the network argument as a request")
	}
}
