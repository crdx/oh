package fit

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/style"
)

func plainly(rungs []string) []string {
	drawn := make([]string, 0, len(rungs))

	for _, rung := range rungs {
		drawn = append(drawn, style.Plain(rung))
	}

	return drawn
}

func TestALadderKeepsEveryRungItIsGiven(t *testing.T) {
	got := Ladder("whole", "less", "least", "")
	if want := []string{"whole", "less", "least", ""}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestALadderDropsARungThatDrawsWhatTheOneAboveItDrew(t *testing.T) {
	got := Ladder("whole", "whole", "less", "less", "")
	if want := []string{"whole", "less", ""}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestALadderStopsAtTheRungThatDrawsNothing(t *testing.T) {
	got := Ladder("whole", "", "less")
	if want := []string{"whole", ""}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPartsHideOneMoreAtEveryRung(t *testing.T) {
	got := plainly(Parts([]string{"one", "two", "three"}))
	want := []string{
		"one, two, three",
		"one, two, +1",
		"one, +2",
		"+3",
		"+",
	}

	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestASolePartIsHiddenBehindItsCountAndThenTheCountAlone(t *testing.T) {
	got := plainly(Parts([]string{"one"}))
	if want := []string{"one", "+1", "+"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoPartsMakeNoLadderAtAll(t *testing.T) {
	if got := Parts(nil); got != nil {
		t.Errorf("got %q, want nothing", got)
	}
}
