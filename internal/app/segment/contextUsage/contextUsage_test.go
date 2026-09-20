package contextUsage_test

import (
	"testing"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/contextUsage"
	"crdx.org/oh/internal/app/style"
)

type noOptions struct{}

func (noOptions) Read(any) error { return nil }

func render(t *testing.T, usedTokens int, totalTokens int) string {
	t.Helper()

	built, err := contextUsage.New(func() (int, int) {
		return usedTokens, totalTokens
	})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return style.Plain(built.Render(segment.Context{}))
}

func TestContextUsageIsDerivedWhenRendered(t *testing.T) {
	usedTokens := 32_000
	built, err := contextUsage.New(func() (int, int) {
		return usedTokens, 274_000
	})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	usedTokens = 64_000
	if got := style.Plain(built.Render(segment.Context{})); got != "23% 64K/274K" {
		t.Errorf("got %q", got)
	}
}

func TestContextUsageShowsEveryKnownPart(t *testing.T) {
	for name, test := range map[string]struct {
		usedTokens  int
		totalTokens int
		want        string
	}{
		"neither":             {want: "?% 0/?"},
		"total only":          {totalTokens: 200_000, want: "0% 0/200K"},
		"one million context": {usedTokens: 500_000, totalTokens: 1_000_000, want: "50% 500K/1M"},
		"used only":           {usedTokens: 5000, want: "?% 5K/?"},
		"both":                {usedTokens: 5000, totalTokens: 200_000, want: "3% 5K/200K"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := render(t, test.usedTokens, test.totalTokens); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestContextUsageIsWrittenToTwoSignificantDigits(t *testing.T) {
	for name, test := range map[string]struct {
		usedTokens  int
		totalTokens int
		want        string
	}{
		"a handful of tokens still counts": {usedTokens: 400, totalTokens: 200_000, want: "0% 1K/200K"},
		"thousands are rounded":            {usedTokens: 92_501, totalTokens: 200_000, want: "46% 93K/200K"},
		"a window over a million":          {usedTokens: 92_000, totalTokens: 1_048_576, want: "9% 92K/1M"},
		"millions keep a decimal":          {usedTokens: 1_600_000, totalTokens: 2_000_000, want: "80% 1.6M/2M"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := render(t, test.usedTokens, test.totalTokens); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestContextUsageRoundsAndCapsItsPercentage(t *testing.T) {
	if got := render(t, 5000, 200_000); got != "3% 5K/200K" {
		t.Errorf("got %q", got)
	}
	if got := render(t, 250_000, 200_000); got != "100% 250K/200K" {
		t.Errorf("got %q", got)
	}
}
