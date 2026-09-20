package util_test

import (
	"testing"

	"crdx.org/oh/internal/util"
)

func TestFormatBytesUsesCompactWholeUnits(t *testing.T) {
	for count, want := range map[uint64]string{
		900: "900B", 1536: "1.5K", 2273: "2.22K", 2 << 20: "2M", 3 << 30: "3G", 4 << 40: "4T",
	} {
		if got := util.FormatBytes(count, 3); got != want {
			t.Errorf("FormatBytes(%d, 3) = %q, want %q", count, got, want)
		}
	}
}

func TestFormatBytesHonoursPrecisionAndClampsNegativeCounts(t *testing.T) {
	if got := util.FormatBytes(2273, 2); got != "2.2K" {
		t.Errorf("got %q, want 2.2K", got)
	}
	if got := util.FormatBytes(100<<10, 2); got != "100K" {
		t.Errorf("got %q, want 100K without scientific notation", got)
	}
	if got := util.FormatBytes(int64(-1), 2); got != "0B" {
		t.Errorf("got %q, want 0B", got)
	}
}
