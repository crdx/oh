package key

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

func decode(t *testing.T, input string) []Key {
	t.Helper()

	decoder := NewDecoder(bufio.NewReader(strings.NewReader(input)))

	var keypresses []Key

	for {
		next, err := decoder.Next()
		if err != nil {
			return keypresses
		}

		keypresses = append(keypresses, next)
	}
}

type decodedKey struct {
	keypress Key
	err      error
}

func decodeNext(decoder *Decoder) <-chan decodedKey {
	decoded := make(chan decodedKey, 1)
	go func() {
		keypress, err := decoder.Next()
		decoded <- decodedKey{keypress: keypress, err: err}
	}()

	return decoded
}

func decodeFragmentedTerminal(t *testing.T, continuation string) Key {
	t.Helper()

	terminal, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.Close() }()
	defer func() { _ = writer.Close() }()

	isWaiting := make(chan struct{})
	decoder := newDecoder(bufio.NewReader(terminal), func() bool {
		close(isWaiting)
		return hasTerminalInput(terminal, escapeSequenceTimeout)
	})
	decoded := decodeNext(decoder)

	if _, err := writer.WriteString("\x1b"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-isWaiting:
	case got := <-decoded:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.keypress
	case <-time.After(time.Second):
		t.Fatal("decoder did not inspect the escape")
		return Key{}
	}

	if _, err := writer.WriteString(continuation); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-decoded:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.keypress
	case <-time.After(time.Second):
		t.Fatal("decoder did not finish")
		return Key{}
	}
}

func TestAnEscapeSequenceMayBeSplitAcrossTerminalReads(t *testing.T) {
	if got := decodeFragmentedTerminal(t, "[A"); got != (Key{Code: Up}) {
		t.Errorf("got %+v, want Up", got)
	}
}

func TestAltEnterMayBeSplitAcrossTerminalReads(t *testing.T) {
	if got := decodeFragmentedTerminal(t, "\r"); got != (Key{Code: Enter, Mod: Alt}) {
		t.Errorf("got %+v, want Alt+Enter", got)
	}
}

func TestABareTerminalEscapeReturnsAfterTheSequenceDeadline(t *testing.T) {
	terminal, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.Close() }()
	defer func() { _ = writer.Close() }()

	decoded := decodeNext(NewTerminalDecoder(bufio.NewReader(terminal), terminal))
	if _, err := writer.WriteString("\x1b"); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-decoded:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.keypress != (Key{Code: Escape}) {
			t.Errorf("got %+v, want Escape", got.keypress)
		}
	case <-time.After(time.Second):
		t.Fatal("bare Escape did not return")
	}
}

func TestFocusReportingIsRestoredWithTheKeyboardProtocol(t *testing.T) {
	if !strings.Contains(Enable, "\x1b[?1004h") {
		t.Errorf("focus reporting is not enabled by %q", Enable)
	}
	if !strings.Contains(Disable, "\x1b[?1004l") {
		t.Errorf("focus reporting is not disabled by %q", Disable)
	}
}

func TestEveryLineEndingIsOneEnter(t *testing.T) {
	for name, input := range map[string]string{
		"cr":   "a\rb",
		"lf":   "a\nb",
		"crlf": "a\r\nb",
	} {
		got := decode(t, input)

		want := []Key{{Code: Rune, Value: 'a'}, {Code: Enter}, {Code: Rune, Value: 'b'}}
		if len(got) != len(want) {
			t.Errorf("%s: expected %d keys, got %d: %v", name, len(want), len(got), got)
			continue
		}

		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: expected %v, got %v", name, want[i], got[i])
			}
		}
	}
}

func TestTabArrivesAsItself(t *testing.T) {
	got := decode(t, "\t")

	if len(got) != 1 || got[0] != (Key{Code: Rune, Value: '\t'}) {
		t.Errorf("expected one tab, got %v", got)
	}
}

func TestControlCharactersStillCarryTheirModifier(t *testing.T) {
	got := decode(t, "\x03")

	if len(got) != 1 || got[0] != (Key{Code: Rune, Value: 'c', Mod: Ctrl}) {
		t.Errorf("expected ctrl+c, got %v", got)
	}
}

func TestLegacyAndKeyboardProtocolEscapesAreReported(t *testing.T) {
	for name, input := range map[string]string{
		"legacy":            "\x1b",
		"keyboard protocol": "\x1b[27u",
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != (Key{Code: Escape}) {
			t.Errorf("%s: expected an Escape, got %v", name, got)
		}
	}
}

func TestLegacyAltPrefixesModifyTheFollowingKey(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1ba":    {Code: Rune, Value: 'a', Mod: Alt},
		"\x1b\r":   {Code: Enter, Mod: Alt},
		"\x1b\x7f": {Code: Backspace, Mod: Alt},
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q: expected %+v, got %v", input, want, got)
		}
	}
}

func TestASequenceIsNotABareEscape(t *testing.T) {
	got := decode(t, "\x1b[A\x1b[27u\x1b[200~\x1b[201~")

	want := []Key{{Code: Up}, {Code: Escape}, {Code: PasteStart}, {Code: PasteEnd}}
	if len(got) != len(want) {
		t.Fatalf("expected %d keys, got %v", len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want[i], got[i])
		}
	}
}

func TestFocusChangesArriveAsKeys(t *testing.T) {
	got := decode(t, "\x1b[I\x1b[O")
	want := []Key{{Code: FocusIn}, {Code: FocusOut}}

	if len(got) != len(want) {
		t.Fatalf("expected %d focus changes, got %v", len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want[i], got[i])
		}
	}
}

func TestApplicationCursorKeysAreArrows(t *testing.T) {
	for input, want := range map[string]Code{
		"\x1bOA": Up,
		"\x1bOB": Down,
		"\x1bOC": Right,
		"\x1bOD": Left,
		"\x1bOH": Home,
		"\x1bOF": End,
	} {
		keypresses := decode(t, input)

		if len(keypresses) != 1 {
			t.Errorf("%q gave %d keys, want one", input, len(keypresses))
			continue
		}

		if keypresses[0].Code != want {
			t.Errorf("%q gave %+v, want code %d", input, keypresses[0], want)
		}
	}
}

func TestLegacyTildeHomeAndEndKeysAreNavigation(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1b[1~":   {Code: Home},
		"\x1b[4~":   {Code: End},
		"\x1b[1;5~": {Code: Home, Mod: Ctrl},
		"\x1b[4;5~": {Code: End, Mod: Ctrl},
	} {
		keypresses := decode(t, input)
		if len(keypresses) != 1 || keypresses[0] != want {
			t.Errorf("%q gave %+v, want %+v", input, keypresses, want)
		}
	}
}

func TestOnlyTheLetterControlsCarryALetter(t *testing.T) {
	if got := plain(0); got.Code != Unknown {
		t.Errorf("plain(0) = %+v, want unknown", got)
	}

	if got := plain(1); got.Code != Rune || got.Value != 'a' || !got.Mod.Has(Ctrl) {
		t.Errorf("plain(1) = %+v, want ctrl+a", got)
	}

	if got := plain(26); got.Code != Rune || got.Value != 'z' || !got.Mod.Has(Ctrl) {
		t.Errorf("plain(26) = %+v, want ctrl+z", got)
	}
}

func TestAPageOfMovementArrivesAsAKey(t *testing.T) {
	got := decode(t, "\x1b[5~\x1b[6~")
	want := []Key{{Code: PageUp}, {Code: PageDown}}

	if len(got) != len(want) {
		t.Fatalf("expected %d keys, got %v", len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %v, got %v", want[i], got[i])
		}
	}
}
