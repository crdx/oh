package money

import "testing"

func TestAnAmountIsDrawnAsMoneyOrAsARate(t *testing.T) {
	for name, test := range map[string]struct {
		dollars float64
		money   string
		rate    string
	}{
		"nothing":                 {dollars: 0, money: "$0.00", rate: "$0.00"},
		"below what is shown":     {dollars: 0.000_01, money: "<$0.0001", rate: "<$0.0001"},
		"the smallest shown":      {dollars: 0.0001, money: "$0.0001", rate: "$0.0001"},
		"a fraction of a cent":    {dollars: 0.0042, money: "$0.0042", rate: "$0.0042"},
		"a few cents":             {dollars: 0.078, money: "$0.08", rate: "$0.078"},
		"a trailing zero goes":    {dollars: 0.02, money: "$0.02", rate: "$0.02"},
		"a quarter of one":        {dollars: 0.25, money: "$0.25", rate: "$0.25"},
		"most of one":             {dollars: 0.3, money: "$0.30", rate: "$0.30"},
		"a round sum keeps pence": {dollars: 3, money: "$3.00", rate: "$3.00"},
		"a half rounds up":        {dollars: 1.25, money: "$1.25", rate: "$1.30"},
		"a tenth is all that is":  {dollars: 3.141_59, money: "$3.14", rate: "$3.10"},
		"a large one":             {dollars: 600, money: "$600.00", rate: "$600.00"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Dollar().Format(test.dollars); got != test.money {
				t.Errorf("as money got %q, want %q", got, test.money)
			}
			if got := Dollar().FormatRate(test.dollars); got != test.rate {
				t.Errorf("as a rate got %q, want %q", got, test.rate)
			}
		})
	}
}

func TestAPriceIsShownToTheNearestPenny(t *testing.T) {
	for dollars, want := range map[float64]string{
		0:        "$0.00",
		0.00001:  "<$0.01",
		0.009999: "<$0.01",
		0.01:     "$0.01",
		0.014:    "$0.01",
		0.015:    "$0.02",
		0.029751: "$0.03",
		1.25:     "$1.25",
		600:      "$600.00",
	} {
		if got := Dollar().FormatPrice(dollars); got != want {
			t.Errorf("FormatPrice(%v) = %q, want %q", dollars, got, want)
		}
	}

	if got := In("GBP", 0.8).FormatPrice(0.029751); got != "£0.02" {
		t.Errorf("got %q", got)
	}
	if got := In("GBP", 0.8).FormatPrice(0.01); got != "<£0.01" {
		t.Errorf("got %q", got)
	}
	if got := (Currency{}).FormatPrice(1.25); got != "$1.25" {
		t.Errorf("got %q", got)
	}
}

func TestAnotherCurrencyIsConvertedAndNamed(t *testing.T) {
	pounds := In("GBP", 0.8)

	if pounds.IsDollar() {
		t.Error("expected pounds to be told from dollars")
	}
	if got := pounds.Format(10); got != "£8.00" {
		t.Errorf("got %q", got)
	}
	if got := pounds.FormatRate(10); got != "£8.00" {
		t.Errorf("got %q", got)
	}

	euros := In("EUR", 0.9)
	if got := euros.FormatRate(1); got != "€0.90" {
		t.Errorf("got %q", got)
	}
}

func TestACurrencyWithoutASymbolIsNamedByItsCode(t *testing.T) {
	if got := In("XYZ", 2).FormatRate(1.5); got != "XYZ 3.00" {
		t.Errorf("got %q", got)
	}
}

func TestAnUnquotedCurrencyStaysInDollars(t *testing.T) {
	for name, currency := range map[string]Currency{
		"no rate":      In("GBP", 0),
		"no code":      In("", 0.8),
		"the dollar":   In("USD", 1),
		"a zero value": {},
	} {
		t.Run(name, func(t *testing.T) {
			if !currency.IsDollar() {
				t.Fatalf("expected dollars, got %+v", currency)
			}
			if got := currency.FormatRate(2); got != "$2.00" {
				t.Errorf("got %q", got)
			}
		})
	}
}
