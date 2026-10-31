package harness

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/vt"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/sandbox"
)

func TestARenderedJournalShowsWhatItsReplayShows(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)
			events := make([]agent.Event, 0, len(entries))
			for _, entry := range entries {
				events = append(events, *entry.Event)
			}

			drawing, err := RenderJournal(events, replayColumns)
			if err != nil {
				t.Fatal(err)
			}

			rendered := vt.New(replayColumns, 0)
			if err := rendered.Play(strings.ReplaceAll(drawing, "\n", "\r\n")); err != nil {
				t.Fatal(err)
			}
			want := slices.DeleteFunc(
				playScreen(t, replayAtWidth(t, entries, replayColumns), replayColumns).text(),
				func(row string) bool { return strings.Contains(row, picturePlaceholder) },
			)
			if got := rendered.Text(); !slices.Equal(got, want) {
				t.Errorf("the rendering showed\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func TestEachRenderedRegionIsNamedAfterTheEventThatDrewIt(t *testing.T) {
	events := []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "read the readme"},
		{Kind: agent.ToolCallRequestEvent, ID: "call_1", Name: "read", Arguments: `{"path":"README.md"}`},
		{Kind: agent.ToolCallResultEvent, ID: "call_1", Name: "read", Status: agent.SuccessStatus, Text: "# Title"},
		{Kind: agent.ModelMessageEvent, Text: "It holds a title."},
	}

	drawing, err := RenderJournal(events, replayColumns)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, part := range strings.Split(drawing, "\x1b]7700;region=")[1:] {
		name, _, _ := strings.Cut(part, "\a")
		names = append(names, name)
	}
	if want := []string{"user", "tool:read", "answer"}; !slices.Equal(names, want) {
		t.Errorf("got regions %q, want %q", names, want)
	}
}

func TestASceneDrawsWhatItsKeysLeaveOnTheScreen(t *testing.T) {
	binary := buildTestBinary(t)
	directory := t.TempDir()
	scene := filepath.Join(directory, "search.toml")
	if err := os.WriteFile(scene, []byte(`
model = "claude-opus-5"
history = ["git status --short", "run the checks", "grep for the spinner"]
keys = ["<ctrl+r>", "run"]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	environment := append(testBinaryEnvironment(t, t.TempDir()), "TMPDIR="+reachableWorkspaceDir(t))
	first := runTestBinary(t, binary, reachableWorkspaceDir(t), environment, "--ctl", "render", scene, "--width", "80", "--height", "8")
	second := runTestBinary(t, binary, reachableWorkspaceDir(t), environment, "--ctl", "render", scene, "--width", "80", "--height", "8")

	text := strings.Join(playScreen(t, strings.ReplaceAll(first, "\n", "\r\n"), 80).text(), "\n")
	for _, want := range []string{"reverse-i-search: run", "run the checks", "tame-impala", "Opus 5"} {
		if !strings.Contains(text, want) {
			t.Errorf("the scene drew no %q:\n%s", want, text)
		}
	}
	if first != second {
		t.Errorf("two drawings of one scene differed:\n%s\n\n%s", first, second)
	}
}

func TestASceneDrawsAConfinedSessionWithoutTheSandbox(t *testing.T) {
	binary := buildTestBinary(t)
	scene := filepath.Join(t.TempDir(), "idle.toml")
	if err := os.WriteFile(scene, []byte(`model = "claude-opus-5"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	environment := append(testBinaryEnvironment(t, t.TempDir()), "TMPDIR="+reachableWorkspaceDir(t))
	drawing := runTestBinary(t, binary, reachableWorkspaceDir(t), environment, "--ctl", "render", scene, "--width", "80", "--height", "6")

	text := strings.Join(playScreen(t, strings.ReplaceAll(drawing, "\n", "\r\n"), 80).text(), "\n")
	if !strings.Contains(text, "rxw snlg") {
		t.Errorf("the scene drew no confined mode toggle:\n%s", text)
	}
}

func TestASceneRunsNoCommands(t *testing.T) {
	runner, keeperProcess, closeKeeper, err := openRunner(t.Context(), false, true)
	defer closeKeeper()
	if err != nil || keeperProcess != nil {
		t.Fatalf("got keeper %v and error %v, want neither", keeperProcess, err)
	}

	if _, err := runner.Run(t.Context(), t.TempDir(), "true", sandbox.Policy{}); !errors.Is(err, errSceneCommand) {
		t.Errorf("got %v from Run, want a refusal", err)
	}
	if _, err := runner.Start(t.Context(), t.TempDir(), "true", sandbox.Policy{}, nil); !errors.Is(err, errSceneCommand) {
		t.Errorf("got %v from Start, want a refusal", err)
	}
}

func TestARenderedJournalIsInColourWhereverItIsWritten(t *testing.T) {
	restore := style.Init(&strings.Builder{})
	defer restore()

	drawing, err := RenderJournal([]agent.Event{{Kind: agent.UserMessageEvent, Text: "hello"}}, replayColumns)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(drawing, "\x1b[48;2;") {
		t.Errorf("the user message had no background: %q", drawing)
	}
	if style.IsColourEnabled() {
		t.Error("rendering left colour forced on")
	}
}
