package util_test

import (
	"testing"

	"crdx.org/oh/internal/util"
)

func TestPluralAddsAnSToEveryCountButOne(t *testing.T) {
	for count, want := range map[int]string{
		0: "0 caches",
		1: "1 cache",
		2: "2 caches",
	} {
		if got := util.Plural(count, "cache"); got != want {
			t.Errorf("Plural(%d, %q) = %q, want %q", count, "cache", got, want)
		}
	}
}

func TestPluralKeepsTheNounItWasGiven(t *testing.T) {
	if got := util.Plural(1, "running session"); got != "1 running session" {
		t.Errorf("Plural(1, %q) = %q", "running session", got)
	}

	if got := util.Plural(3, "running session"); got != "3 running sessions" {
		t.Errorf("Plural(3, %q) = %q", "running session", got)
	}
}

func TestPluralNounCarriesNoCount(t *testing.T) {
	for count, want := range map[int]string{
		0: "caches",
		1: "cache",
		2: "caches",
	} {
		if got := util.PluralNoun(count, "cache"); got != want {
			t.Errorf("PluralNoun(%d, %q) = %q, want %q", count, "cache", got, want)
		}
	}
}
