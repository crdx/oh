package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/snippets"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

func TestGoldenCompletionMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), fixtureSnippets())
	var output strings.Builder

	for _, prefix := range []string{
		"/",
		"/c",
		"/g",
		"/grant ",
		"/grants ",
		"/job ",
		"/r",
		"/revoke ",
		"/copy ",
		"/copy l",
		"/copy sn",
		"/edit ",
		"/edit sn",
		"/open ",
		"/open sn",
		"//",
		"//a",
		"//h",
	} {
		fmt.Fprintf(&output, "%q\n", prefix)
		for _, completion := range commands.Completions(prefix) {
			fmt.Fprintf(&output, "  %s → %s", completion.Label, completion.Text)
			if completion.TakesArguments {
				output.WriteString(" …")
			}
			if completion.Description != "" {
				fmt.Fprintf(&output, "  (%s)", completion.Description)
			}
			output.WriteByte('\n')
		}
	}

	assertGolden(t, "completion.txt", output.String())
}

func TestGoldenContextListingMatchesGolden(t *testing.T) {
	var output strings.Builder
	for _, test := range []struct {
		label   string
		sources ContextSources
	}{
		{
			label: "every context category",
			sources: ContextSources{
				SystemSources: []contextsource.Source{
					{Name: "harness", EstimatedTokens: 12_000},
					{Path: "/config/SYSTEM.md", EstimatedTokens: 2_000},
					{Name: "tool definitions (12 tools)", EstimatedTokens: 300},
				},
				ProjectSources: []contextsource.Source{{
					Path: "/workspace/AGENTS.md", EstimatedTokens: 4_000,
				}},
				SessionSources: []contextsource.Source{
					{Name: "skill catalogue (29 skills)", EstimatedTokens: 300},
					{Path: "/config/skills/golang/SKILL.md", EstimatedTokens: 1_000},
				},
			},
		},
		{
			label: "system sources only",
			sources: ContextSources{SystemSources: []contextsource.Source{
				{Name: "harness", EstimatedTokens: 4_000},
				{Name: "tool definitions (4 tools)", EstimatedTokens: 300},
			}},
		},
		{
			label: "project sources only",
			sources: ContextSources{ProjectSources: []contextsource.Source{{
				Path: "/workspace/AGENTS.md", EstimatedTokens: 4_000,
			}}},
		},
		{
			label: "session sources only",
			sources: ContextSources{SessionSources: []contextsource.Source{{
				Path: "/config/skills/todo/SKILL.md", EstimatedTokens: 1_000,
			}}},
		},
		{label: "no context sources"},
	} {
		commands := newCommandRegistry(t, commandEnvironment{
			getContextSources: func() ContextSources { return test.sources },
		})
		invocation, found := commands.Find("/ctx")
		if !found {
			t.Fatal("expected /ctx to be registered")
		}
		context := &commandTestContext{}
		if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
			t.Fatal(err)
		}
		if !context.isListing {
			t.Error("context sources were not marked as a listing")
		}
		for _, columns := range []int{120, 60, 30} {
			fmt.Fprintf(
				&output,
				"=== %s (%d columns) ===\n%s\n",
				test.label,
				columns,
				renderInformationListing(context.notice, columns),
			)
		}
	}

	assertGolden(t, "context.txt", output.String())
}

func TestEveryCommandIsDescribedInOneLowercaseSentence(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), nil)

	descriptions := map[string]string{}
	for _, prefix := range []string{"/", "//"} {
		for _, completion := range commands.Completions(prefix) {
			descriptions[completion.Label] = completion.Description
		}
	}
	invocation, found := commands.Find(systemCommandPrefix + shellCommandName + "ls")
	if !found {
		t.Fatal("expected /! to be registered")
	}
	descriptions[invocation.Name] = invocation.Command.Description

	for name, description := range descriptions {
		isLowercase := description != "" && description == strings.ToLower(description)
		if !isLowercase || strings.ContainsAny(description, ".;:!?") {
			t.Errorf("%s is described as %q", name, description)
		}
	}
}

func renderInformationListing(text string, columns int) string {
	var shown feedback.State
	shown.Show(feedback.Command, feedback.Message{
		Text:      text,
		Status:    agent.InfoStatus,
		IsListing: true,
	}, time.Time{})
	return strings.Join(shown.Render(columns, time.Time{}), "\n")
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
		permanent     []shell.ScopedPathGrant
		temporary     []pathgrant.Grant
		currentCaps   caps.Set
		denyPatterns  []string
		isYolo        bool
		isExpanded    bool
		hostToSandbox []uint16
		sandboxToHost []uint16
	}{
		{
			label: "effective paths and ports in both directions",
			permanent: []shell.ScopedPathGrant{
				{Path: "/commands", Access: pathgrant.ReadAccess | pathgrant.ExecAccess, Kind: shell.ExecutableSearchGrant},
				{Path: "/reference", Access: pathgrant.ReadAccess, Kind: shell.ConfiguredGrant},
			},
			temporary: []pathgrant.Grant{
				{Path: "/output", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
				{Path: "/reference", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
			},
			hostToSandbox: []uint16{3000},
			sandboxToHost: []uint16{8080},
		},
		{
			label: "every permanent path kind",
			permanent: []shell.ScopedPathGrant{
				{Path: "/cache", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.PrivateCacheGrant},
				{Path: "/cache", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.UnixSocketGrant},
				{Path: "/drops", Access: shell.ReadAccess, Kind: shell.SessionDropsGrant},
				{Path: "/go-mod", Access: shell.ReadAccess, Kind: shell.LanguageModuleGrant},
				{Path: "/home", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.PrivateHomeGrant},
				{Path: "/repo/**/.git", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.RepositoryMetadataGrant},
				{Path: "/runtime", Access: shell.ReadAccess, Kind: shell.RuntimeGrant},
				{Path: "/ptys", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.RuntimeGrant},
				{Path: "/skill/one", Access: shell.ReadAccess, Kind: shell.GlobalSkillGrant},
				{Path: "/skill/two", Access: shell.ReadAccess, Kind: shell.GlobalSkillGrant},
				{Path: "/etc/one", Access: shell.ReadAccess, Kind: shell.SystemGrant},
				{Path: "/dev/null", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.SystemGrant},
				{Path: "/usr", Access: shell.ReadAccess | shell.ExecAccess, Kind: shell.SystemGrant},
				{Path: "/tmp", Access: shell.ReadAccess | shell.ExecAccess | shell.WriteAccess, Kind: shell.TemporaryDirectoryGrant},
				{Path: "/tmp", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.UnixSocketGrant},
			},
			currentCaps: caps.All(),
		},
		{
			label: "expanded built-in paths",
			permanent: []shell.ScopedPathGrant{
				{Path: "/path/one", Access: shell.ReadAccess | shell.ExecAccess, Kind: shell.ExecutableSearchGrant},
				{Path: "/path/two", Access: shell.ReadAccess | shell.ExecAccess, Kind: shell.ExecutableSearchGrant},
				{Path: "/modules/cache", Access: shell.ReadAccess, Kind: shell.LanguageModuleGrant},
				{Path: "/runtime/process", Access: shell.ReadAccess, Kind: shell.RuntimeGrant},
				{Path: "/runtime/ptys", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.RuntimeGrant},
				{Path: "/skills/one", Access: shell.ReadAccess, Kind: shell.GlobalSkillGrant},
				{Path: "/skills/two", Access: shell.ReadAccess, Kind: shell.GlobalSkillGrant},
				{Path: "/system/read", Access: shell.ReadAccess, Kind: shell.SystemGrant},
				{Path: "/system/devices", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.SystemGrant},
				{Path: "/system/bin", Access: shell.ReadAccess | shell.ExecAccess, Kind: shell.SystemGrant},
			},
			isExpanded: true,
		},
		{
			label:       "read-only workspace",
			permanent:   []shell.ScopedPathGrant{{Path: "/workspace", Access: shell.ReadAccess, Kind: shell.WorkspaceGrant}},
			currentCaps: caps.Read,
		},
		{
			label: "writable workspace and denied names",
			permanent: []shell.ScopedPathGrant{{
				Path: "/workspace", Access: shell.ReadAccess | shell.WriteAccess, Kind: shell.WorkspaceGrant,
			}},
			currentCaps:  caps.Read | caps.Write,
			denyPatterns: []string{".env", "*.pem"},
		},
		{
			label: "yolo deny scope",
			permanent: []shell.ScopedPathGrant{
				{Path: "/", Access: shell.ReadAccess | shell.ExecAccess | shell.WriteAccess, Kind: shell.UnconfinedShellGrant},
				{
					Path: "/workspace", Access: shell.ReadAccess | shell.ExecAccess | shell.WriteAccess, Kind: shell.WorkspaceGrant,
				},
			},
			currentCaps:  caps.Read | caps.Shell | caps.Write,
			denyPatterns: []string{".env"},
			isYolo:       true,
		},
		{label: "host to sandbox alone", hostToSandbox: []uint16{8080}},
		{label: "sandbox to host alone", sandboxToHost: []uint16{3000}},
		{label: "nothing granted"},
	} {
		pathGrants, current := fixturePathGrants()
		pathGrants.Permanent = test.permanent
		pathGrants.DenyPatterns = test.denyPatterns
		pathGrants.IsYolo = test.isYolo
		if test.currentCaps != 0 {
			pathGrants.GetCurrentCaps = func() caps.Set { return test.currentCaps }
		}
		*current = test.temporary
		hostToSandbox, hostExposed := fixturePortGrants()
		*hostExposed = test.hostToSandbox
		sandboxToHost, sandboxExposed := fixtureSandboxToHost()
		*sandboxExposed = test.sandboxToHost
		commands := newCommandRegistry(t, commandEnvironment{
			pathGrants:    pathGrants,
			hostToSandbox: hostToSandbox,
			sandboxToHost: sandboxToHost,
		})
		input := "/grants"
		if test.isExpanded {
			input += " all"
		}
		invocation, found := commands.Find(input)
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
	failedJob := func(name string, command string) jobs.Snapshot {
		snapshot := finishedJob(name, command)
		snapshot.State = jobs.StateFailed
		snapshot.ExitCode = 1
		return snapshot
	}
	stoppedJob := func(name string, command string) jobs.Snapshot {
		snapshot := finishedJob(name, command)
		snapshot.State = jobs.StateStopped
		snapshot.ExitCode = -1
		snapshot.Failure = "the command was stopped after 22m46s\nnote: the command was killed by SIGKILL."
		return snapshot
	}

	var output strings.Builder
	for _, test := range []struct {
		label   string
		listing []jobs.Snapshot
	}{
		{label: "no jobs"},
		{label: "ordinary and multiline commands", listing: []jobs.Snapshot{
			finishedJob("build", "GOCACHE=/tmp/cache just build && echo done"),
			failedJob("docs", "python3 -m http.server 8080\n  --bind localhost"),
		}},
		{label: "heredoc command", listing: []jobs.Snapshot{
			finishedJob("write", "cat <<'EOF' > notes.txt\nhello\nEOF"),
		}},
		{label: "malformed stored command", listing: []jobs.Snapshot{
			finishedJob("broken", "echo 'unterminated\necho later"),
		}},
		{label: "multiline failure", listing: []jobs.Snapshot{
			stoppedJob("witness", "python3 /tmp/model-witness.py"),
		}},
	} {
		managedJobs, _ := fixtureJobs()
		managedJobs.List = func() []jobs.Snapshot { return test.listing }

		context, err := invokeJobCommand(t, managedJobs, "/jobs")
		if err != nil {
			t.Fatal(err)
		}

		for _, columns := range []int{120, 60, 30} {
			var shown feedback.State
			shown.Show(feedback.Command, feedback.Message{
				Text:      context.notice,
				Status:    agent.InfoStatus,
				IsListing: context.isListing,
			}, startedAt)
			rows := shown.Render(columns, startedAt)
			wantedRows := 1
			if len(test.listing) > 0 {
				wantedRows += len(test.listing)
			}
			if len(rows) != wantedRows {
				t.Errorf("%s at %d columns drew %d rows, want %d", test.label, columns, len(rows), wantedRows)
			}
			for rowNumber, row := range rows {
				rowColumns := width.Of(row)
				if rowColumns > columns {
					t.Errorf("%s row %d uses %d columns, want at most %d", test.label, rowNumber, rowColumns, columns)
				}
			}
			fmt.Fprintf(
				&output,
				"=== %s (%d columns) ===\n%s\n",
				test.label,
				columns,
				strings.Join(rows, "\n"),
			)
		}
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

func (self *helpContext) NoticeIndented(text string, _ int) {
	self.notice = text
}

func (self *helpContext) NoticeListing(text string) {
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
func (self *promptContext) Notice(string)              {}
func (self *promptContext) NoticeIndented(string, int) {}
func (self *promptContext) NoticeListing(string)       {}
func (self *promptContext) PlainNotice(string)         {}
func (self *promptContext) Success(string)             {}

func TestGoldenInfoMatchesGolden(t *testing.T) {
	commands := newCommandRegistry(t, fixtureEnvironment(t))
	invocation, found := commands.Find("/info")
	if !found {
		t.Fatal("expected /info to be registered")
	}

	context := &commandTestContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if !context.isListing {
		t.Error("session info was not marked as a listing")
	}
	assertGolden(t, "info.txt", renderInformationListing(context.notice, 80)+"\n")
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
