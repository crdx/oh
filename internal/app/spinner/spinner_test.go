package spinner_test

import (
	"testing"
	"time"

	"crdx.org/oh/internal/app/spinner"
	"crdx.org/oh/internal/app/style"
)

func TestActivityIsTwoCellsWide(t *testing.T) {
	for frame := range 4 {
		value := spinner.Activity.Frame(frame)
		if width := style.Width(value); width != 2 {
			t.Errorf("frame %d is %d cells wide: %q", frame, width, value)
		}
	}
}

func TestActivityMovesAtATwinklingPace(t *testing.T) {
	if interval := spinner.Activity.RefreshInterval(); interval != 125*time.Millisecond {
		t.Errorf("got interval %s, want 125ms", interval)
	}
}
