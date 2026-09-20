package usage

import (
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
)

func TestAWindowIsLabelledByHowLongItRuns(t *testing.T) {
	for _, test := range []struct {
		duration time.Duration
		want     string
	}{
		{duration: 90 * time.Minute, want: "90m"},
		{duration: 5 * time.Hour, want: "5h"},
		{duration: 7 * 24 * time.Hour, want: "7d"},
		{duration: 30 * 24 * time.Hour, want: "30d"},
		{duration: 36 * time.Hour, want: "36h"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := DurationLabel(test.duration); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestWhatHasBeenSpentIsWeighedAgainstHowFarTheWindowHasRun(t *testing.T) {
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name   string
		window agent.UsageWindow
		want   int
	}{
		{
			name:   "a window half run",
			window: agent.UsageWindow{Duration: 4 * time.Hour, ResetsAt: at.Add(2 * time.Hour)},
			want:   50,
		},
		{
			name:   "a window not yet started",
			window: agent.UsageWindow{Duration: time.Hour, ResetsAt: at.Add(2 * time.Hour)},
			want:   0,
		},
		{
			name:   "a window already over",
			window: agent.UsageWindow{Duration: time.Hour, ResetsAt: at.Add(-time.Hour)},
			want:   100,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ExpectedPercent(test.window, at); got != test.want {
				t.Errorf("got %d, want %d", got, test.want)
			}
		})
	}
}

func TestSpendingIsClassifiedAgainstThePaceTheWindowExpects(t *testing.T) {
	for _, test := range []struct {
		name     string
		actual   int
		expected int
		want     Pace
	}{
		{name: "barely spent at all", actual: 5, expected: 0, want: PaceEven},
		{name: "spending under the pace", actual: 20, expected: 50, want: PaceEven},
		{name: "spending a little over the pace", actual: 40, expected: 30, want: PaceAhead},
		{name: "spending far over the pace", actual: 60, expected: 30, want: PaceCritical},
		{name: "close to the limit", actual: 95, expected: 90, want: PaceCritical},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyPace(test.actual, test.expected); got != test.want {
				t.Errorf("got %d, want %d", got, test.want)
			}
		})
	}
}
