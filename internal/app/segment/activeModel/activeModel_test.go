package activeModel_test

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/activeModel"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

type noOptions struct{}

func (noOptions) Read(any) error { return nil }

func ladder(t *testing.T, settings activeModel.Settings) []string {
	t.Helper()

	built, err := activeModel.New(settings)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	fitter, isFitter := built.(segment.Fitter)
	if !isFitter {
		t.Fatal("the active model segment does not fit itself to available room")
	}

	drawn := []string{}
	for _, rung := range fitter.Ladder(segment.Context{}) {
		drawn = append(drawn, style.Plain(rung))
	}

	return drawn
}

func TestTheModelShortensBeforeItsThinkingIsShed(t *testing.T) {
	got := ladder(t, activeModel.Settings{
		Name:         "claude-opus-5",
		Effort:       "medium",
		EffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
	})
	want := []string{"Opus 5 ·▫▪▫▫▫", "O5 ·▫▪▫▫▫", "O5", ""}

	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAModelWithNoThinkingToShowShortensAndThenGoes(t *testing.T) {
	got := ladder(t, activeModel.Settings{Name: "llama3.3:70b"})
	want := []string{"Llama 3.3 70B", "L3.3 70B", ""}

	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheFastMarkStandsOnEveryRungTheModelDoes(t *testing.T) {
	got := ladder(t, activeModel.Settings{
		Name:         "gpt-5.6-sol",
		Effort:       "high",
		EffortLevels: []string{"minimal", "low", "medium", "high", "xhigh", "max"},
		IsFast:       true,
	})
	want := []string{"⚡ GPT Sol 5.6 ▫▫▫▪▫▫", "⚡ GPTS5.6 ▫▫▫▪▫▫", "⚡ GPTS5.6", ""}

	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func FuzzEveryModelShortensWithoutWidening(fuzzer *testing.F) {
	for _, seed := range []string{
		"claude-opus-5",
		"gpt-5.6-sol",
		"ollama/qwen3.8:27b",
		"",
		"日本語-2:70b",
		"👨‍👩‍👧‍👦-4",
		"🇬🇧",
		"e\u0301clair-2",
		"simulation",
	} {
		for _, effort := range []string{"", "high", "max"} {
			fuzzer.Add(seed, effort, byte(0b101010), false)
		}
	}

	fuzzer.Fuzz(func(t *testing.T, name string, effort string, levels byte, isFast bool) {
		if len(name) > 256 {
			t.Skip("longer than a model is ever named")
		}

		effortLevels := []string{}
		for at, level := range model.EffortOrder {
			if levels&(1<<at) != 0 {
				effortLevels = append(effortLevels, level)
			}
		}

		rungs := ladder(t, activeModel.Settings{
			Name:         name,
			Effort:       effort,
			EffortLevels: effortLevels,
			IsFast:       isFast,
		})

		if len(rungs) == 0 {
			t.Fatal("a model drew no ladder at all")
		}

		for at, rung := range rungs {
			if at > 0 && rung == rungs[at-1] {
				t.Fatalf("%q drew %q twice over", name, rung)
			}

			if at > 0 && width.Of(rung) > width.Of(rungs[0]) {
				t.Fatalf("%q widened from %q to %q", name, rungs[0], rung)
			}

			if rung == "" && at != len(rungs)-1 {
				t.Fatalf("%q went on drawing after rung %d drew nothing", name, at)
			}
		}

		if last := rungs[len(rungs)-1]; last != "" {
			t.Fatalf("%q ends its ladder at %q rather than nothing", name, last)
		}
	})
}
