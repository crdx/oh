package util_test

import (
	"testing"

	"crdx.org/oh/internal/util"
)

func TestFormatCountScalesOnlyBeyondAThousand(t *testing.T) {
	for value, want := range map[int64]string{
		-1:         "0",
		0:          "0",
		1:          "1",
		999:        "999",
		1000:       "1K",
		9564:       "9.6K",
		22_138:     "22K",
		56_545:     "57K",
		999_999:    "1000K",
		1_000_000:  "1M",
		12_345_678: "12M",
	} {
		if got := util.FormatCount(value); got != want {
			t.Errorf("FormatCount(%d) = %q, want %q", value, got, want)
		}
	}
}
