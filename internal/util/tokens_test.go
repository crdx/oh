package util_test

import (
	"testing"

	"crdx.org/oh/internal/util"
)

func TestTokensEstimatedFromBytesUseTheMeasuredRate(t *testing.T) {
	for bytes, want := range map[int64]string{
		-1:   "0t",
		0:    "0t",
		1:    "~1t",
		2:    "~1t",
		3:    "~2t",
		740:  "~300t",
		4000: "~1.4Kt",
		5000: "~1.8Kt",
	} {
		if got := util.FormatEstimatedTokens(util.EstimateTokenCount(bytes)); got != want {
			t.Errorf("FormatEstimatedTokens(estimate of %d) = %q, want %q", bytes, got, want)
		}
	}
}

func TestFormatEstimatedTokensRoundsSubKiloValuesToNearestHundred(t *testing.T) {
	for tokens, want := range map[int64]string{
		0:         "0t",
		1:         "~1t",
		9:         "~9t",
		10:        "~100t",
		49:        "~100t",
		50:        "~100t",
		949:       "~900t",
		950:       "~1Kt",
		999:       "~1Kt",
		1000:      "~1Kt",
		1_000_000: "~1Mt",
		1_234_567: "~1.2Mt",
	} {
		if got := util.FormatEstimatedTokens(tokens); got != want {
			t.Errorf("FormatEstimatedTokens(%d) = %q, want %q", tokens, got, want)
		}
	}
}

func TestAnEstimateIsWrittenToTwoSignificantDigits(t *testing.T) {
	if got := util.FormatEstimatedTokens(util.EstimateTokenCount(4996)); got != "~1.8Kt" {
		t.Errorf("got %q, want ~1.8Kt", got)
	}
	if got := util.FormatEstimatedTokens(util.EstimateTokenCount(400_000)); got != "~143Kt" {
		t.Errorf("got %q, want ~143Kt without scientific notation", got)
	}
}

func TestFormatCountCarriesNoUnitAndSaysNothingUsedAsNothing(t *testing.T) {
	for tokens, want := range map[int64]string{
		-1:            "0",
		0:             "0",
		400:           "400",
		5000:          "5K",
		64_000:        "64K",
		92_501:        "93K",
		274_000:       "274K",
		1_048_576:     "1M",
		1_600_000:     "1.6M",
		1_500_000_000: "1.5B",
	} {
		if got := util.FormatCount(tokens); got != want {
			t.Errorf("FormatCount(%d) = %q, want %q", tokens, got, want)
		}
	}
}

func TestFormatTokensCarriesTheUnitAndSaysNothingUsedAsNothing(t *testing.T) {
	for tokens, want := range map[int64]string{
		-1:        "0t",
		0:         "0t",
		300:       "300t",
		400:       "400t",
		5000:      "5Kt",
		294_000:   "294Kt",
		1_600_000: "1.6Mt",
	} {
		if got := util.FormatTokens(tokens); got != want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tokens, got, want)
		}
	}
}

func TestImageTokensAreEstimatedFromPatches(t *testing.T) {
	for name, test := range map[string]struct {
		width  int
		height int
		want   int64
	}{
		"invalid":       {0, 100, 0},
		"one patch":     {32, 32, 1},
		"partial patch": {33, 32, 2},
		"capped":        {2098, 1136, 1536},
	} {
		t.Run(name, func(t *testing.T) {
			if got := util.EstimateImageTokenCount(test.width, test.height); got != test.want {
				t.Errorf("got %d, want %d", got, test.want)
			}
		})
	}
}
