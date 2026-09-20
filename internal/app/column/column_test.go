package column_test

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/column"
	"crdx.org/oh/internal/app/width"
)

func TestValuesAreLinedUpInColumnsThatFit(t *testing.T) {
	got := column.Rows([]string{"one", "three", "seventeen", "two", "four"}, 24)
	want := []string{
		"one        three",
		"seventeen  two",
		"four",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoRowIsWiderThanItWasAskedFor(t *testing.T) {
	values := []string{"config-dir", "config-file", "home-dir", "scratch-dir", "session-chat"}
	for _, cells := range []int{1, 14, 20, 40, 80} {
		for _, row := range column.Rows(values, cells) {
			if width.Of(row) > cells && width.Of(row) > len("session-chat") {
				t.Errorf("at %d cells got row %q", cells, row)
			}
		}
	}
}

func TestAValueTooWideToFitStillGetsARowOfItsOwn(t *testing.T) {
	got := column.Rows([]string{"first", "second"}, 2)
	want := []string{"first", "second"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNothingLaidOutIsNoRows(t *testing.T) {
	if got := column.Rows(nil, 40); got != nil {
		t.Errorf("got %q", got)
	}
}

func TestAWideCharacterIsPaddedByWhatItDraws(t *testing.T) {
	got := column.Rows([]string{"🚀", "ab", "c"}, 8)
	want := []string{"🚀  ab", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
