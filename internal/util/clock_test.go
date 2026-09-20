package util_test

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/util"
)

const monotonicReading = " m=+"

func TestTheWallClockKeepsTheMomentAndDropsTheMonotonicReading(t *testing.T) {
	taken := time.Now()
	if !strings.Contains(taken.String(), monotonicReading) {
		t.Fatalf("got %q, want a time carrying a monotonic reading", taken)
	}

	onTheWall := util.WallClock(taken)

	if got := onTheWall.String(); strings.Contains(got, monotonicReading) {
		t.Errorf("got %q, want no monotonic reading", got)
	}

	if !onTheWall.Equal(taken) {
		t.Errorf("got %q, want %q", onTheWall, taken)
	}
}
