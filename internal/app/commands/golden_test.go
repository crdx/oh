package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/pathgrant"
	"crdx.org/io/internal/app/slash"
	"crdx.org/io/internal/app/snippets"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
)

func TestGoldenCompletionMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), fixtureSnippets())
	var output strings.Builder

	for _, test := range []struct {
		prefix string
		steps  int
	}{
		{prefix: "/", steps: 13},
		{prefix: "/c", steps: 2},
		{prefix: "/g", steps: 2},
		{prefix: "/grant ", steps: 4},
		{prefix: "/r", steps: 1},
		{prefix: "/revoke ", steps: 1},
		{prefix: "/copy ", steps: 3},
		{prefix: "/copy l", steps: 1},
		{prefix: "/copy sn", steps: 1},
		{prefix: "/edit ", steps: 3},
		{prefix: "/edit sn", steps: 1},
		{prefix: "/open ", steps: 12},
		{prefix: "/open sn", steps: 1},
		{prefix: "//", steps: 4},
		{prefix: "//a", steps: 3},
		{prefix: "//h", steps: 1},
	} {
		state := slash.Completion{}
		current := test.prefix
		fmt.Fprintf(&output, "%s", test.prefix)
		for range test.steps {
			completed, found := state.Next(commands, current)
			if !found {
				t.Fatalf("expected completion for %q", current)
			}
			fmt.Fprintf(&output, " → %s", completed)
			current = completed
		}
		output.WriteByte('\n')
	}

	assertGolden(t, "completion.txt", output.String())
}

func assertGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

func fixtureEnvironment(t *testing.T) commandEnvironment {
	t.Helper()
	configDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(configDirectory, "snippets"), 0o700); err != nil {
		t.Fatal(err)
	}
	grants, current := fixturePathGrants()
	*current = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, exposed := fixturePortGrants()
	*exposed = []uint16{8080}
	managedJobs, _ := fixtureJobs()
	return commandEnvironment{
		configDir:     configDirectory,
		pathGrants:    grants,
		hostToSandbox: ports,
		jobs:          managedJobs,
		getInfo: func() (string, error) {
			return "cache-usage  5m ttl\nmode-toggle  rxw ngl", nil
		},
	}
}

func fixtureSnippets() map[string]snippets.Definition {
	return map[string]snippets.Definition{
		"add": {
			Prompt:      "Add {{.Arg}}",
			Description: "Add a task, then continue.",
			Arguments:   snippets.ArgumentsRequired,
		},
		"ask": {
			Prompt:      "Ask {{.Arg}}",
			Description: "Answer without making changes.",
			Arguments:   snippets.ArgumentsRequired,
		},
		"note": {
			Prompt: "Note this.",
		},
	}
}

func TestGoldenGrantListingMatchesGolden(t *testing.T) {
	var output strings.Builder
	for _, test := range []struct {
		label         string
		grants        []pathgrant.Grant
		hostToSandbox []uint16
		sandboxToHost []uint16
	}{
		{label: "paths and ports in both directions", grants: []pathgrant.Grant{
			{Path: "/reference", Access: pathgrant.ReadAccess},
			{Path: "/output", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
		}, hostToSandbox: []uint16{3000}, sandboxToHost: []uint16{8080}},
		{label: "paths alone", grants: []pathgrant.Grant{
			{Path: "/reference", Access: pathgrant.ReadAccess},
		}},
		{label: "host to sandbox alone", hostToSandbox: []uint16{8080}},
		{label: "sandbox to host alone", sandboxToHost: []uint16{3000}},
		{label: "nothing granted"},
	} {
		pathGrants, current := fixturePathGrants()
		*current = test.grants
		hostToSandbox, hostExposed := fixturePortGrants()
		*hostExposed = test.hostToSandbox
		sandboxToHost, sandboxExposed := fixtureSandboxToHost()
		*sandboxExposed = test.sandboxToHost
		commands := newCommandRegistry(t, commandEnvironment{
			pathGrants:    pathGrants,
			hostToSandbox: hostToSandbox,
			sandboxToHost: sandboxToHost,
		})
		invocation, found := commands.Find("/grants")
		if !found {
			t.Fatal("expected /grants to be registered")
		}
		context := &helpContext{}
		if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %s ===\n%s\n", test.label, context.notice)
	}
	assertGolden(t, "grants.txt", output.String())
}

func TestGoldenSnippetHelpMatchesGolden(t *testing.T) {
	var output strings.Builder
	for _, test := range []struct {
		label              string
		configuredSnippets map[string]snippets.Definition
	}{
		{label: "snippets configured", configuredSnippets: fixtureSnippets()},
		{label: "no snippets configured", configuredSnippets: nil},
	} {
		commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), test.configuredSnippets)
		invocation, found := commands.Find("//help")
		if !found {
			t.Fatal("expected //help to be registered")
		}

		context := &helpContext{}
		if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %s ===\n%s\n", test.label, context.notice)
	}

	assertGolden(t, "snippet-help.txt", output.String())
}

func expansionSnippets() map[string]snippets.Definition {
	return map[string]snippets.Definition{
		"add":  {Prompt: "Add the following:\n\n{{.Arg}}"},
		"note": {Prompt: "Note this."},
		"tag":  {Prompt: "{{range .Args}}[{{.}}]{{end}}"},
	}
}

func TestGoldenSnippetExpansionMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), expansionSnippets())
	var output strings.Builder

	for _, input := range []string{
		"//note",
		"//add pay the bill",
		"//add   spaced   out   words  ",
		"//add first line\n\nsecond paragraph\n  - one\n  - two\n",
		"//add\nnothing on the command line\n",
		"//tag one two three",
	} {
		invocation, found := commands.Find(input)
		if !found {
			t.Fatalf("expected %q to be found", input)
		}

		context := &promptContext{}
		if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %q ===\n%s\n", input, context.sent)
	}

	assertGolden(t, "snippet-expansion.txt", output.String())
}

func TestGoldenJobListingMatchesGolden(t *testing.T) {
	startedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	finishedJob := func(name string, command string) jobs.Snapshot {
		return jobs.Snapshot{
			Name:      name,
			Command:   command,
			State:     jobs.StateComplete,
			StartedAt: startedAt,
			EndedAt:   startedAt.Add(3 * time.Second),
		}
	}

	var output strings.Builder
	for _, test := range []struct {
		label   string
		listing []jobs.Snapshot
	}{
		{label: "no jobs"},
		{label: "ordinary and multiline commands", listing: []jobs.Snapshot{
			finishedJob("build", "GOCACHE=/tmp/cache just build && echo done"),
			finishedJob("docs", "python3 -m http.server 8080\n  --bind localhost"),
		}},
		{label: "heredoc command", listing: []jobs.Snapshot{
			finishedJob("write", "cat <<'EOF' > notes.txt\nhello\nEOF"),
		}},
		{label: "malformed stored command", listing: []jobs.Snapshot{
			finishedJob("broken", "echo 'unterminated\necho later"),
		}},
	} {
		managedJobs, _ := fixtureJobs()
		managedJobs.List = func() []jobs.Snapshot { return test.listing }

		context, err := invokeJobCommand(t, managedJobs, "/jobs")
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %s ===\n%s\n", test.label, context.notice)
	}

	assertGolden(t, "job-listing.txt", output.String())
}

func TestGoldenJobOutputMatchesGolden(t *testing.T) {
	var output strings.Builder
	for _, test := range []struct {
		label  string
		output string
	}{
		{label: "with output", output: "built successfully\n"},
		{label: "without output", output: " \n\t"},
	} {
		managedJobs, _ := fixtureJobs()
		managedJobs.Output = func(string) (string, jobs.Snapshot, error) {
			return test.output, jobs.Snapshot{Name: "build", State: jobs.StateComplete}, nil
		}
		context, err := invokeJobCommand(t, managedJobs, "/job output build")
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %s ===\n%s\n", test.label, context.notice)
	}

	assertGolden(t, "job-output.txt", output.String())
}

var updateGoldens = flag.Bool("update", false, "write command output back to the golden files")

type helpContext struct {
	notice string
}

func (self *helpContext) Emit(agent.Event) {}
func (self *helpContext) Send(string)      {}
func (self *helpContext) Notice(text string) {
	self.notice = text
}

func (self *helpContext) PlainNotice(text string) {
	self.notice = text
}

func (self *helpContext) Success(string) {}

type promptContext struct {
	sent string
}

func (self *promptContext) Emit(agent.Event) {}
func (self *promptContext) Send(prompt string) {
	self.sent = prompt
}
func (self *promptContext) Notice(string)      {}
func (self *promptContext) PlainNotice(string) {}
func (self *promptContext) Success(string)     {}

func TestGoldenInfoMatchesGolden(t *testing.T) {
	commands := newCommandRegistry(t, fixtureEnvironment(t))
	invocation, found := commands.Find("/info")
	if !found {
		t.Fatal("expected /info to be registered")
	}

	context := &helpContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "info.txt", context.notice+"\n")
}

func TestGoldenHelpMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), fixtureSnippets())
	invocation, found := commands.Find("/help")
	if !found {
		t.Fatal("expected /help to be registered")
	}

	context := &helpContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "help.txt", style.Plain(context.notice)+"\n")
	assertGolden(t, "help.ansi", strutil.VisibleEscapes(context.notice)+"\n")
}
