package terminal

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/work"
)

func interactiveTitle(writer *strings.Builder) *title {
	return &title{writer: writer, isTerminal: true}
}

func TestTerminalBeginsUpdatesAndRestoresItsTitle(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := Terminal{title: interactiveTitle(output), workspaceName: "project"}

	restore := managedTerminal.Begin(caps.Read | caps.Write)
	managedTerminal.SetMode(caps.Read)
	if got, want := output.String(), pushTitle+"\x1b]2;project ✱\x1b\\\x1b]2;project\x1b\\"; got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}

	output.Reset()
	restore()
	if got := output.String(); got != popTitle {
		t.Errorf("got restore sequence %q, want %q", got, popTitle)
	}
}

func TestTitleCannotContainTerminalControlCharacters(t *testing.T) {
	output := &strings.Builder{}
	managedTitle := interactiveTitle(output)

	managedTitle.Begin("oh\n\x1b]2;elsewhere\a")

	if got, want := output.String(), pushTitle+"\x1b]2;oh]2;elsewhere\x1b\\"; got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}
}

func TestTheTerminalIsNamedAfterTheWorkspace(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := New(output, work.At("/work/project"))
	managedTerminal.title = interactiveTitle(output)

	restore := managedTerminal.Begin(caps.Read)
	defer restore()

	if got, want := output.String(), pushTitle+"\x1b]2;project\x1b\\"; got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}
}

func TestTerminalDoesNotWriteItsTitleToRedirectedOutput(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := New(output, work.At("/work/project"))

	restore := managedTerminal.Begin(caps.Read | caps.Write)
	managedTerminal.SetMode(caps.Read | caps.Write | caps.Git)
	restore()

	if output.Len() != 0 {
		t.Errorf("expected no title sequence, got %q", output.String())
	}
}

func TestScrollbackIsNotResetOnRedirectedOutput(t *testing.T) {
	output := &strings.Builder{}

	ResetScrollback(output)

	if output.Len() != 0 {
		t.Errorf("expected no erase sequence, got %q", output.String())
	}
}

func TestTheSessionTitleStandsInTheWindowTitle(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := Terminal{title: interactiveTitle(output), workspaceName: "project"}

	restore := managedTerminal.Begin(caps.Read)
	managedTerminal.SetSessionTitle("fix the picker clipping")
	managedTerminal.SetSessionTitle("fix the picker clipping")
	managedTerminal.SetMode(caps.Read | caps.Write)
	defer restore()

	want := pushTitle +
		"\x1b]2;project\x1b\\" +
		"\x1b]2;fix the picker clipping\x1b\\" +
		"\x1b]2;fix the picker clipping ✱\x1b\\"
	if got := output.String(); got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}
	if got := managedTerminal.GetSessionTitle(); got != "fix the picker clipping" {
		t.Errorf("the terminal is going by %q", got)
	}
}

func TestATitleTakenBeforeTheTerminalIsHeldWaitsForIt(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := Terminal{title: interactiveTitle(output), workspaceName: "project"}

	managedTerminal.SetSessionTitle("fix the picker clipping")
	if output.Len() != 0 {
		t.Fatalf("the title was written over before it was kept: %q", output.String())
	}

	restore := managedTerminal.Begin(caps.Read)
	if got, want := output.String(), pushTitle+"\x1b]2;fix the picker clipping\x1b\\"; got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}

	output.Reset()
	restore()
	managedTerminal.SetSessionTitle("give sessions a title")
	if got := output.String(); got != popTitle {
		t.Errorf("got restore sequence %q, want %q", got, popTitle)
	}
}
