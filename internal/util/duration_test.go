package util_test

import (
	"testing"
	"time"

	"crdx.org/oh/internal/util"
)

func TestCompactDurationDropsAnEmptySmallerUnit(t *testing.T) {
	cases := map[time.Duration]string{
		0:                                  "0s",
		1200 * time.Millisecond:            "1s",
		30 * time.Second:                   "30s",
		time.Minute:                        "1m",
		time.Minute + 100*time.Millisecond: "1m",
		9500 * time.Millisecond:            "9s",
		5 * time.Minute:                    "5m",
		time.Hour + 400*time.Millisecond:   "1h",
		90 * time.Second:                   "1m30s",
		time.Hour:                          "1h",
		time.Hour + 30*time.Minute:         "1h30m",
		100 * time.Hour:                    "4d04h",
	}

	for took, want := range cases {
		if got := util.CompactDuration(took); got != want {
			t.Errorf("CompactDuration(%s) = %q, want %q", took, got, want)
		}
	}
}

func TestCoarseDurationUsesCompactUnits(t *testing.T) {
	cases := map[time.Duration]string{
		0:                           "<1m",
		59 * time.Second:            "<1m",
		37 * time.Minute:            "37m",
		5*time.Hour + 1*time.Minute: "5h",
		73 * time.Hour:              "3d",
	}

	for elapsed, want := range cases {
		if got := util.CoarseDuration(elapsed); got != want {
			t.Errorf("CoarseDuration(%s) = %q, want %q", elapsed, got, want)
		}
	}
}

func TestAgoNamesTheMostRecentMomentAsNow(t *testing.T) {
	cases := map[time.Duration]string{
		0:                "just now",
		30 * time.Second: "just now",
		37 * time.Minute: "37m ago",
		5 * time.Hour:    "5h ago",
		73 * time.Hour:   "3d ago",
		1000 * time.Hour: "41d ago",
	}

	for elapsed, want := range cases {
		if got := util.Ago(time.Now().Add(-elapsed)); got != want {
			t.Errorf("Ago(%s ago) = %q, want %q", elapsed, got, want)
		}
	}
}
