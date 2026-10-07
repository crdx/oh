package editor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfigurationCanReplaceItsEditorCommand(t *testing.T) {
	initial := Command{"first-editor", "--wait"}
	configuration := NewConfiguration(initial)
	initial[0] = "mutated"

	if got := configuration.GetCommand(); !slices.Equal(got, Command{"first-editor", "--wait"}) {
		t.Errorf("got initial command %v", got)
	}

	replacement := Command{"second-editor"}
	configuration.ReplaceCommand(replacement)
	replacement[0] = "mutated"
	got := configuration.GetCommand()
	if !slices.Equal(got, Command{"second-editor"}) {
		t.Errorf("got replacement command %v", got)
	}
	got[0] = "mutated"
	if current := configuration.GetCommand(); !slices.Equal(current, Command{"second-editor"}) {
		t.Errorf("returned command changed the configuration to %v", current)
	}
}

func environment(variables map[string]string) func(string) string {
	return func(name string) string { return variables[name] }
}

func onPath(t *testing.T, names ...string) {
	t.Helper()

	directory := t.TempDir()
	for _, name := range names {
		writeExecutable(t, directory, name, "#!/bin/sh\n")
	}
	t.Setenv("PATH", directory)
}

func TestTheEditorIsResolvedFromWhatIsAvailable(t *testing.T) {
	display := map[string]string{"DISPLAY": ":0"}
	wayland := map[string]string{"WAYLAND_DISPLAY": "wayland-0"}
	cases := map[string]struct {
		configured Command
		installed  []string
		variables  map[string]string
		want       Launch
	}{
		"a configured graphical editor": {
			configured: Command{" subl ", "--wait"},
			want:       Launch{Command: Command{"subl", "--wait"}},
		},
		"a configured terminal editor": {
			configured: Command{"/usr/bin/vim"},
			want:       Launch{Command: Command{"/usr/bin/vim"}, IsTerminal: true},
		},
		"a configured editor even without a display": {
			configured: Command{"code", "--wait"},
			installed:  []string{"vim"},
			want:       Launch{Command: Command{"code", "--wait"}},
		},
		"sublime under X": {
			installed: []string{"subl", "code", "vim"},
			variables: display,
			want:      Launch{Command: Command{"subl", "--wait"}},
		},
		"vim ahead of code under Wayland": {
			installed: []string{"code", "vim"},
			variables: wayland,
			want:      Launch{Command: Command{"vim"}, IsTerminal: true},
		},
		"code under Wayland without vim": {
			installed: []string{"code", "nano"},
			variables: wayland,
			want:      Launch{Command: Command{"code", "--wait"}},
		},
		"no code without a display": {
			installed: []string{"code", "zed", "nano"},
			want:      Launch{Command: Command{"nano"}, IsTerminal: true},
		},
		"sublime ahead of EDITOR under X": {
			installed: []string{"subl", "vim"},
			variables: map[string]string{"DISPLAY": ":0", "EDITOR": "nano"},
			want:      Launch{Command: Command{"subl", "--wait"}},
		},
		"emacs kept in the terminal": {
			installed: []string{"emacs"},
			variables: display,
			want:      Launch{Command: Command{"emacs", "-nw"}, IsTerminal: true},
		},
		"vi as the last resort": {
			installed: []string{"vi"},
			want:      Launch{Command: Command{"vi"}, IsTerminal: true},
		},
		"VISUAL without a display": {
			installed: []string{"subl", "vim"},
			variables: map[string]string{"VISUAL": "nvim -u NONE", "EDITOR": "nano"},
			want:      Launch{Command: Command{"nvim", "-u", "NONE"}, IsTerminal: true},
		},
		"EDITOR without a display": {
			installed: []string{"subl"},
			variables: map[string]string{"VISUAL": "  ", "EDITOR": "nano"},
			want:      Launch{Command: Command{"nano"}, IsTerminal: true},
		},
		"EDITOR under a display with no graphical editor": {
			installed: []string{"vim"},
			variables: map[string]string{"DISPLAY": ":0", "EDITOR": "micro"},
			want:      Launch{Command: Command{"micro"}, IsTerminal: true},
		},
		"vim over ssh": {
			installed: []string{"subl", "nano", "nvim", "vim"},
			want:      Launch{Command: Command{"vim"}, IsTerminal: true},
		},
		"nano when it is all there is": {
			installed: []string{"nano"},
			want:      Launch{Command: Command{"nano"}, IsTerminal: true},
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			onPath(t, test.installed...)

			got, err := Resolve(test.configured, environment(test.variables))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Command, test.want.Command) || got.IsTerminal != test.want.IsTerminal {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestNoEditorIsFoundWhenNothingIsAvailable(t *testing.T) {
	onPath(t, "subl")

	_, err := Resolve(Command{" "}, environment(nil))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestThePositionCountsLinesAndColumnsFromOne(t *testing.T) {
	cases := map[string]struct {
		text   string
		cursor int
		want   Position
	}{
		"at the start":         {text: "hello", cursor: 0, want: Position{Line: 1, Column: 1, ByteColumn: 1}},
		"at the end":           {text: "hello", cursor: 5, want: Position{Line: 1, Column: 6, ByteColumn: 6}},
		"on a later line":      {text: "one\ntwo\nthree", cursor: 10, want: Position{Line: 3, Column: 3, ByteColumn: 3}},
		"after a newline":      {text: "one\n", cursor: 4, want: Position{Line: 2, Column: 1, ByteColumn: 1}},
		"after wide letters":   {text: "x\ncafé au", cursor: 7, want: Position{Line: 2, Column: 6, ByteColumn: 7}},
		"beyond the text":      {text: "ab", cursor: 9, want: Position{Line: 1, Column: 3, ByteColumn: 3}},
		"before the beginning": {text: "ab", cursor: -1, want: Position{Line: 1, Column: 1, ByteColumn: 1}},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := PositionIn([]rune(test.text), test.cursor); got != test.want {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestEachEditorIsToldWhereTheCursorStands(t *testing.T) {
	position := Position{Line: 3, Column: 5, ByteColumn: 7}
	cases := map[string]struct {
		command Command
		want    []string
	}{
		"vim":    {command: Command{"vim"}, want: []string{"+call cursor(3, 7)", "draft.md"}},
		"nvim":   {command: Command{"/usr/bin/nvim"}, want: []string{"+call cursor(3, 7)", "draft.md"}},
		"nano":   {command: Command{"nano"}, want: []string{"+3,5", "draft.md"}},
		"ne":     {command: Command{"ne"}, want: []string{"+3,5", "draft.md"}},
		"emacs":  {command: Command{"emacs", "-nw"}, want: []string{"-nw", "+3:4", "draft.md"}},
		"micro":  {command: Command{"micro"}, want: []string{"+3:5", "draft.md"}},
		"hx":     {command: Command{"hx"}, want: []string{"draft.md:3:5"}},
		"helix":  {command: Command{"helix"}, want: []string{"draft.md:3:5"}},
		"subl":   {command: Command{"subl", "--wait"}, want: []string{"--wait", "draft.md:3:5"}},
		"zed":    {command: Command{"zed", "--wait"}, want: []string{"--wait", "draft.md:3:5"}},
		"code":   {command: Command{"code", "--wait"}, want: []string{"--wait", "--goto", "draft.md:3:5"}},
		"vi":     {command: Command{"vi"}, want: []string{"+3", "draft.md"}},
		"joe":    {command: Command{"joe"}, want: []string{"+3", "draft.md"}},
		"mcedit": {command: Command{"mcedit"}, want: []string{"+3", "draft.md"}},
		"kak":    {command: Command{"kak"}, want: []string{"draft.md"}},
		"other":  {command: Command{"ed"}, want: []string{"draft.md"}},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			launch := Launch{Command: test.command, IsTerminal: true, Position: position}
			if got := launch.Arguments([]string{"draft.md"}); !slices.Equal(got, test.want) {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestOnlyALoneFileIsOpenedAtThePosition(t *testing.T) {
	launch := Launch{Command: Command{"subl"}, Position: Position{Line: 2, Column: 1, ByteColumn: 1}}
	if got, want := launch.Arguments([]string{"a.md", "b.md"}), []string{"a.md", "b.md"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	unplaced := Launch{Command: Command{"vim"}}
	if got, want := unplaced.Arguments([]string{"a.md"}), []string{"a.md"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q without a position", got, want)
	}
}

func TestEveryPathIsPassedToAGraphicalEditor(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "config.toml")

	launch := Launch{Command: Command{"/usr/bin/subl", "--wait", "--background"}}
	got := launch.Arguments([]string{directory, file})
	if want := []string{"--wait", "--background", directory, file}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestATerminalEditorIsGivenOnlyFilesBesideDirectories(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	launch := Launch{Command: Command{"vim", "-p"}, IsTerminal: true}
	if got, want := launch.Arguments([]string{directory, file}), []string{"-p", file}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := launch.Arguments([]string{directory}), []string{"-p", directory}; !slices.Equal(got, want) {
		t.Errorf("got %v, want the directory, since nothing else was asked for", got)
	}
}

func TestAGraphicalEditorIsNotTakenForATerminalEditor(t *testing.T) {
	for _, name := range []string{"subl", "code", "/usr/bin/gvim", "zed"} {
		if IsTerminalEditor(name) {
			t.Errorf("%s was taken for a terminal editor", name)
		}
	}
}

func TestAGraphicalEditorThatFailsSaysWhy(t *testing.T) {
	directory := t.TempDir()
	editorPath := writeExecutable(t, directory, "subl",
		"#!/bin/sh\necho 'starting up' >&2\necho 'Timeout waiting for detached instance to start' >&2\nexit 1\n")

	err := Launch{Command: Command{editorPath}}.Run(t.Context(), []string{"/nowhere"}, Terminal{})
	if err == nil || err.Error() != "subl: Timeout waiting for detached instance to start" {
		t.Errorf("got %v", err)
	}
}

func TestAGraphicalEditorThatSaysNothingNamesItsExit(t *testing.T) {
	directory := t.TempDir()
	editorPath := writeExecutable(t, directory, "code", "#!/bin/sh\nexit 42\n")

	err := Launch{Command: Command{editorPath}}.Run(t.Context(), nil, Terminal{})
	if err == nil || err.Error() != "code: exit status 42" {
		t.Errorf("got %v", err)
	}
}

func TestAnEditorThatCannotStartSaysSo(t *testing.T) {
	err := Launch{Command: Command{filepath.Join(t.TempDir(), "missing-editor")}}.Run(t.Context(), nil, Terminal{})
	if err == nil || !strings.Contains(err.Error(), "missing-editor: ") {
		t.Errorf("got %v", err)
	}
}

func TestAnAbandonedEditorIsReportedAsCancelled(t *testing.T) {
	directory := t.TempDir()
	editorPath := writeExecutable(t, directory, "subl", "#!/bin/sh\nexec sleep 30\n")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Launch{Command: Command{editorPath}}.Run(ctx, nil, Terminal{}) }()
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
}

func TestTheFailureTailKeepsOnlyTheEnd(t *testing.T) {
	failure := &tail{limit: 8}
	_, _ = failure.Write([]byte("a long first line\nlast\n"))

	if got := failure.lastLine(); got != "last" {
		t.Errorf("got %q", got)
	}
}

func writeExecutable(t *testing.T, directory string, name string, script string) string {
	t.Helper()

	path := filepath.Join(directory, name)
	//nolint:gosec // it has to be executable to run
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	return path
}
