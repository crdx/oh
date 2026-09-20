package menu

import (
	"bytes"
	"testing"

	"crdx.org/io/internal/app/style"
)

func TestMenuRenderingReturnsEveryLineToColumnZero(t *testing.T) {
	restoreStyle := style.Init(&bytes.Buffer{})
	t.Cleanup(restoreStyle)

	got := RenderMenu("Choose:", []string{"one", "two"}, 1)
	want := "Choose:\r\n\r\n  one\r\n› two\r\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMenuPresentationAlwaysRestoresTheCursorAndStyle(t *testing.T) {
	var output bytes.Buffer
	restore, err := beginMenuPresentation(&output)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != hideCursor {
		t.Errorf("menu began with %q", output.String())
	}

	restore()
	want := hideCursor + restoreMenuPresentation
	if output.String() != want {
		t.Errorf("menu lifecycle wrote %q, want %q", output.String(), want)
	}
}
