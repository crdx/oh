package edit

import (
	"strings"
	"testing"
	"unicode"

	"crdx.org/oh/internal/app/key"
)

const fuzzedPasteLength = 512

func FuzzPastedTextCarriesNoInstructionForTheTerminal(fuzzer *testing.F) {
	for _, seed := range []string{
		"",
		"hello",
		"one\r\ntwo\rthree",
		"ls\x1b[2J -la",
		"\x1b]52;c;cHduZWQ=\x07",
		"\x1b]0;title\x1b\\",
		"tab\there",
		"    indented\n        further",
		"\x00\x01\x02\x7f",
		"🦫 unicode",
	} {
		fuzzer.Add(seed)
	}

	fuzzer.Fuzz(func(t *testing.T, pasted string) {
		if len(pasted) > fuzzedPasteLength {
			t.Skip("longer than a paste is ever read here")
		}

		requireDrawableInput(t, pastedThroughClipboard(pasted))
		requireDrawableInput(t, pastedThroughTheTerminal(pasted))
	})
}

func pastedThroughClipboard(pasted string) string {
	input := NewInput(NewHistory("", 0))
	input.InsertPasted(pasted)

	return input.Text()
}

func pastedThroughTheTerminal(pasted string) string {
	input := NewInput(NewHistory("", 0))

	input.Apply(key.Key{Code: key.PasteStart}, false)
	for _, character := range pasted {
		if character == '\n' {
			input.Apply(key.Key{Code: key.Enter}, false)
			continue
		}
		input.Apply(key.Key{Code: key.Rune, Value: character}, false)
	}
	input.Apply(key.Key{Code: key.PasteEnd}, false)

	return input.Text()
}

func requireDrawableInput(t *testing.T, text string) {
	t.Helper()

	if strings.ContainsAny(text, "\x1b\r") {
		t.Fatalf("the input holds an escape: %q", text)
	}

	for _, character := range text {
		if character == '\n' || character == '\t' {
			continue
		}
		if unicode.IsControl(character) {
			t.Fatalf("the input holds %q: %q", character, text)
		}
	}
}
