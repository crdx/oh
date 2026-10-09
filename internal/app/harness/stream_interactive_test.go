package harness

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/sim"
)

func TestAThoughtStreamedFasterThanAFrameLeavesTheScreenItsResumeDraws(t *testing.T) {
	thought := strings.Repeat("The footer waits for its frame while the words keep arriving. ", 80)
	rig := newScriptedRig(t, sim.Turn{Think: []string{thought}, Say: "Streamed answer."})

	live := rig.start("--yolo", "-m", "opencode-go/fake", "think it over")
	live.waitFor("Streamed answer.")
	liveScreen := live.screen()
	live.quit()

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want the one that streamed", len(storedSessions))
	}

	resumed := rig.start("-r", storedSessions[0].Name)
	resumed.waitFor("Streamed answer.")
	resumedScreen := resumed.screen()
	resumed.quit()

	if !strings.Contains(strings.Join(liveScreen, "\n"), "2% 4.1K/200K") {
		t.Errorf("the footer never caught up with the last of the stream:\n%s", strings.Join(liveScreen, "\n"))
	}

	if got, want := visibleRows(liveScreen), visibleRows(resumedScreen); !slices.Equal(got, want) {
		t.Errorf("the streamed screen differs from its resume:\nstreamed:\n%s\nresumed:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func visibleRows(screen []string) []string {
	return screen[max(len(screen)-interactiveRows, 0):]
}
