package schedule_test

import (
	"testing"
	"time"

	"crdx.org/io/internal/app/schedule"
)

func TestSoonestIgnoresZeroTimesAndPicksTheEarliest(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	got := schedule.Soonest(time.Time{}, at.Add(2*time.Second), at.Add(time.Second), time.Time{})
	if want := at.Add(time.Second); !got.Equal(want) {
		t.Errorf("got %s, want the earliest at %s", got, want)
	}
}

func TestSoonestOfNothingButZeroTimesIsZero(t *testing.T) {
	if got := schedule.Soonest(time.Time{}, time.Time{}); !got.IsZero() {
		t.Errorf("got %s, want zero", got)
	}
}

func TestNextTickIsTheNextWholeGranularityBoundary(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 500_000_000, time.UTC)

	if got, want := schedule.NextTick(at, time.Second), at.Truncate(time.Second).Add(time.Second); !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}

	if got, want := schedule.NextTick(at, time.Minute), at.Truncate(time.Minute).Add(time.Minute); !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}
