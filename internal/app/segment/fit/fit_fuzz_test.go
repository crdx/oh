package fit

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
)

const (
	fuzzedPartSeparator = "\n"
	fuzzedPartsLength   = 512
)

func withoutRepetition(rungs []string) []string {
	kept := make([]string, 0, len(rungs))

	for _, rung := range rungs {
		if len(kept) > 0 && kept[len(kept)-1] == rung {
			continue
		}

		kept = append(kept, rung)

		if rung == "" {
			break
		}
	}

	return kept
}

func FuzzEveryRungHidesOneMorePartThanTheOneAboveIt(fuzzer *testing.F) {
	for _, seed := range []string{
		"one\ntwo\nthree",
		"one",
		"",
		"\n",
		"a\na\na",
		"日本語\n한국어",
		"👨‍👩‍👧‍👦\n🇬🇧",
		"e\u0301\n\u0301",
		"\u200d\nx",
		"+1\n+2",
		"rw:output\nr:reference\nrx:tools",
		strings.Repeat("wide", 40) + "\nnarrow",
	} {
		fuzzer.Add(seed)
	}

	fuzzer.Fuzz(func(t *testing.T, written string) {
		if len(written) > fuzzedPartsLength {
			t.Skip("longer than a bar is ever drawn")
		}

		parts := strings.Split(written, fuzzedPartSeparator)
		ladder := Parts(parts)

		candidates := []string{strings.Join(parts, style.Subtle(separator))}

		for shownCount := range slices.Backward(parts) {
			hiddenCount := strconv.Itoa(len(parts) - shownCount)
			shown := append(slices.Clone(parts[:shownCount]), style.Subtle("+"+hiddenCount))
			candidates = append(candidates, strings.Join(shown, style.Subtle(separator)))
		}

		candidates = append(candidates, style.Subtle("+"))

		if want := withoutRepetition(candidates); !slices.Equal(ladder, want) {
			t.Fatalf("%q made the ladder %q, want %q", parts, ladder, want)
		}

		for at, rung := range ladder {
			if rung == "" && at != len(ladder)-1 {
				t.Fatalf("%q went on drawing after rung %d drew nothing", parts, at)
			}
		}

		if again := Parts(parts); !slices.Equal(again, ladder) {
			t.Fatalf("%q made two different ladders", parts)
		}
	})
}

func FuzzALadderStopsAtTheRungThatDrawsNothing(fuzzer *testing.F) {
	for _, seed := range []string{
		"whole\nless\nleast\n",
		"whole\nwhole\n\nless",
		"\n\n\n",
		"",
		"日本語\n日\n",
		"🇬🇧\n🇬\n",
		"e\u0301\ne\n",
	} {
		fuzzer.Add(seed)
	}

	fuzzer.Fuzz(func(t *testing.T, written string) {
		if len(written) > fuzzedPartsLength {
			t.Skip("longer than a bar is ever drawn")
		}

		rungs := strings.Split(written, fuzzedPartSeparator)
		ladder := Ladder(rungs...)

		if len(ladder) > len(rungs) {
			t.Fatalf("%q made the longer ladder %q", rungs, ladder)
		}

		for at, rung := range ladder {
			if at > 0 && rung == ladder[at-1] {
				t.Fatalf("%q drew %q twice over", rungs, rung)
			}

			if rung == "" && at != len(ladder)-1 {
				t.Fatalf("%q went on drawing after rung %d drew nothing", rungs, at)
			}

			if !slices.Contains(rungs, rung) {
				t.Fatalf("%q made up the rung %q", rungs, rung)
			}
		}

		if again := Ladder(ladder...); !slices.Equal(again, ladder) {
			t.Fatalf("%q did not settle: %q became %q", rungs, ladder, again)
		}
	})
}
