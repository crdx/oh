package sessionSpend_test

import (
	"testing"

	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/segment/sessionSpend"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/money"
)

type noOptions struct{}

func (noOptions) Read(any) error { return nil }

func render(t *testing.T, dollars float64, isKnown bool) string {
	t.Helper()

	built, err := sessionSpend.New(func() (float64, bool) {
		return dollars, isKnown
	}, money.Dollar())(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return style.Plain(built.Render(segment.Context{}))
}

func TestAnUnpricedModelDrawsNothing(t *testing.T) {
	if got := render(t, 0, false); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

func TestSpendIsDrawnAsMoney(t *testing.T) {
	for name, test := range map[string]struct {
		dollars float64
		want    string
	}{
		"nothing spent yet":  {dollars: 0, want: "$0.00"},
		"a fraction of one":  {dollars: 0.0042, want: "$0.0042"},
		"most of one":        {dollars: 0.875, want: "$0.88"},
		"several":            {dollars: 12.5, want: "$12.50"},
		"rounded to a penny": {dollars: 3.14159, want: "$3.14"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := render(t, test.dollars, true); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestSpendIsDrawnInTheChosenCurrency(t *testing.T) {
	built, err := sessionSpend.New(func() (float64, bool) {
		return 4, true
	}, money.In("GBP", 0.75))(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "£3.00" {
		t.Errorf("got %q", got)
	}
}

func TestSpendIsDerivedWhenRendered(t *testing.T) {
	dollars := 1.0
	built, err := sessionSpend.New(func() (float64, bool) {
		return dollars, true
	}, money.Dollar())(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	dollars = 2.0
	if got := style.Plain(built.Render(segment.Context{})); got != "$2.00" {
		t.Errorf("got %q", got)
	}
}
