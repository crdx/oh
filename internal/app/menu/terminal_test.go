package menu

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"crdx.org/oh/internal/app/ptytest"
)

const (
	pressDown  = "\x1b[B"
	pressUp    = "\x1b[A"
	pressHome  = "\x1b[H"
	pressEnd   = "\x1b[F"
	pressLeft  = "\x1b[D"
	pressEnter = "\r"

	choiceDeadline = 5 * time.Second
)

type chosen struct {
	index int
	err   error
}

func chooseOverATerminal(t *testing.T, rows int, typed string, choose func(*os.File) (int, error)) chosen {
	t.Helper()

	controller, terminal := ptytest.OpenSized(t, 80, rows)
	isDrawing := make(chan struct{})
	go func() {
		var first [1]byte
		if _, err := controller.Read(first[:]); err == nil {
			close(isDrawing)
		}
		_, _ = io.Copy(io.Discard, controller)
	}()

	result := make(chan chosen, 1)
	go func() {
		index, err := choose(terminal)
		result <- chosen{index: index, err: err}
	}()

	select {
	case <-isDrawing:
	case <-time.After(choiceDeadline):
		t.Fatal("nothing was drawn to choose from")
	}
	if _, err := controller.WriteString(typed); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-result:
		return got
	case <-time.After(choiceDeadline):
		t.Fatalf("nothing was chosen after typing %q", typed)
		return chosen{}
	}
}

func TestAMenuOverATerminalScrollsAndChoosesWhereTheCursorStands(t *testing.T) {
	labels := []string{"one", "two", "three", "four", "five"}
	typed := pressDown + pressDown + pressDown + pressUp + "x" + pressLeft + pressEnd + pressHome + pressDown + pressEnter

	got := chooseOverATerminal(t, 6, typed, func(terminal *os.File) (int, error) {
		return ChooseIndex(terminal, terminal, "Choose:", labels)
	})

	if got.err != nil || got.index != 1 {
		t.Errorf("got %d, %v; want the second row", got.index, got.err)
	}
}

func TestAMenuOverATerminalIsAbandonedByQAndByCtrlC(t *testing.T) {
	for name, typed := range map[string]string{"q": "q", "ctrl+c": "\x03"} {
		t.Run(name, func(t *testing.T) {
			got := chooseOverATerminal(t, 24, typed, func(terminal *os.File) (int, error) {
				return ChooseIndex(terminal, terminal, "Choose:", []string{"one", "two"})
			})

			if !errors.Is(got.err, ErrCancelled) {
				t.Errorf("got %d, %v; want the choice abandoned", got.index, got.err)
			}
		})
	}
}

func TestAMenuNeedsATerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "menu")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	if _, err := ChooseIndex(file, io.Discard, "Choose:", []string{"one"}); err == nil {
		t.Error("a menu was drawn over a file")
	}
	if _, err := ChooseIndex(nil, io.Discard, "Choose:", []string{"one"}); err == nil {
		t.Error("a menu was drawn over nothing")
	}
}

func TestAListOverATerminalChoosesTheRowTheCursorReaches(t *testing.T) {
	rows := &fakeList{rows: []string{"alpha", "beta", "gamma"}}

	got := chooseOverATerminal(t, 24, pressDown+pressDown+pressEnter, func(terminal *os.File) (int, error) {
		return Choose(rows, terminal, terminal)
	})

	if got.err != nil || got.index != 2 {
		t.Errorf("got %d, %v; want the third row", got.index, got.err)
	}
}

func TestAListIsMeasuredFromItsTerminal(t *testing.T) {
	_, terminal := ptytest.OpenSized(t, 72, 18)

	columns, rows := measuring(terminal)()
	if columns != 72 || rows != 18 {
		t.Errorf("measured %dx%d, want 72x18", columns, rows)
	}

	file, err := os.CreateTemp(t.TempDir(), "list")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	columns, rows = measuring(file)()
	if columns != defaultColumns || rows != defaultRows {
		t.Errorf("measured %dx%d from a file, want the defaults", columns, rows)
	}
}
