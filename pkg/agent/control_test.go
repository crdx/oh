package agent

import (
	"strings"
	"testing"
)

func TestProseCarriesNoControlCharacters(t *testing.T) {
	const injection = "before \x1b]52;c;cHduZWQ=\x07 and \x1b[2J after"

	var prose proseStream

	var updates []Update
	for _, fragment := range strings.SplitAfter(injection, " ") {
		updates = append(updates, prose.add(Output{Kind: ModelMessageEvent, Text: fragment})...)
	}
	updates = append(updates, prose.add(Output{Kind: ModelMessageEvent, Done: true})...)

	var answer strings.Builder
	for _, update := range updates {
		if update.Delta != nil {
			answer.WriteString(update.Delta.Text)
			assertPlain(t, "a delta", update.Delta.Text)
		}
		if update.Event != nil {
			assertPlain(t, "an event", update.Event.Text)
		}
	}

	if want := "before  and  after"; answer.String() != want {
		t.Errorf("the deltas spelled %q, want %q", answer.String(), want)
	}
}

func assertPlain(t *testing.T, what string, text string) {
	t.Helper()

	for _, character := range text {
		if character != '\n' && character != '\t' && character < ' ' {
			t.Errorf("%s carried %q in %q", what, character, text)
		}
	}
}
