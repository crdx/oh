package turn

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
)

func TestAPassageOfTimeIsToldInItsLargestUnit(t *testing.T) {
	for elapsedTime, want := range map[time.Duration]string{
		time.Second:                     "About 1 second passed",
		45 * time.Second:                "About 45 seconds passed",
		90 * time.Second:                "About 1 minute passed",
		59*time.Minute + 59*time.Second: "About 59 minutes passed",
		time.Hour:                       "About 1 hour passed",
		89 * time.Minute:                "About 1 hour passed",
		90 * time.Minute:                "About 2 hours passed",
		47 * time.Hour:                  "About 47 hours passed",
		48 * time.Hour:                  "About 2 days passed",
		9*24*time.Hour + 13*time.Hour:   "About 10 days passed",
		400*24*time.Hour + 3*time.Hour:  "About 400 days passed",
	} {
		event, isRecorded := PassageEvent(elapsedTime)
		if !isRecorded {
			t.Fatalf("%s was not recorded", elapsedTime)
		}
		notice, isSaid := PassageNotice(event)
		if !isSaid || !strings.HasPrefix(notice, want+" since the previous turn ended. ") {
			t.Errorf("%s was told as %q, want it to open %q", elapsedTime, notice, want)
		}
		if !strings.HasSuffix(notice, passageAdvice) {
			t.Errorf("%s was told as %q, want the advice to check again", elapsedTime, notice)
		}
	}
}

func TestOnlyAPassageOfTimeIsToldAsOne(t *testing.T) {
	if notice, isSaid := PassageNotice(PokeEvent()); isSaid {
		t.Errorf("a poke was told as %q", notice)
	}
	if notice, isSaid := PassageNotice(agent.Event{Kind: TimePassage, State: []byte("{")}); isSaid {
		t.Errorf("an unreadable passage was told as %q", notice)
	}
}
