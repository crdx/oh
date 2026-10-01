package slash_test

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/slash"
)

func TestQuotedFieldsSplitAtSpacesOutsideQuotes(t *testing.T) {
	for text, want := range map[string][]string{
		"":                    {},
		"   ":                 {},
		"/one":                {"/one"},
		" /one \t /two ":      {"/one", "/two"},
		`"/two words" /three`: {"/two words", "/three"},
		`/one "" /three`:      {"/one", "", "/three"},
		`/it"s`:               {`/it"s`},
		`"/closed"/next`:      {"/closed", "/next"},
		`"/tab	and  spaces"`:  {"/tab\tand  spaces"},
	} {
		got, isWellFormed := slash.QuotedFields(text)
		if !isWellFormed || !slices.Equal(got, want) {
			t.Errorf("%q gave %q, %t, want %q", text, got, isWellFormed, want)
		}
	}
}

func TestQuotedFieldsRefuseAnUnclosedQuote(t *testing.T) {
	for _, text := range []string{`"`, `/one "/two`, `"/one" "`} {
		if got, isWellFormed := slash.QuotedFields(text); isWellFormed {
			t.Errorf("%q gave %q", text, got)
		}
	}
}
