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

func TestAnEscapeSequenceMayCrossADelayedTerminalRead(t *testing.T) {
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

	const delay = 50 * time.Millisecond
	if escapeSequenceTimeout <= delay {
		t.Fatalf("escape timeout %v does not cover the test delay", escapeSequenceTimeout)
	}
	time.Sleep(delay)

	if _, err := writer.WriteString("[A"); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-decoded:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.keypress != (Key{Code: Up}) {
			t.Errorf("got %+v, want Up", got.keypress)
		}
	case <-time.After(time.Second):
		t.Fatal("decoder did not finish")
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

func TestBothLegacyBackspaceBytesAreBackspace(t *testing.T) {
	for _, input := range []string{"\b", "\x7f"} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != (Key{Code: Backspace}) {
			t.Errorf("%q: expected Backspace, got %v", input, got)
		}
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

func TestKeyboardProtocolControlKeysKeepTheirModifiers(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1b[13;2u":  {Code: Enter, Mod: Shift},
		"\x1b[99;5u":  {Code: Rune, Value: 'c', Mod: Ctrl},
		"\x1b[127;5u": {Code: Backspace, Mod: Ctrl},
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q: expected %+v, got %v", input, want, got)
		}
	}
}

func TestKeyboardProtocolKeypadKeysMatchTheirOrdinaryKeys(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1b[57414u":   {Code: Enter},
		"\x1b[57414;3u": {Code: Enter, Mod: Alt},
		"\x1b[57417u":   {Code: Left},
		"\x1b[57418u":   {Code: Right},
		"\x1b[57419u":   {Code: Up},
		"\x1b[57420u":   {Code: Down},
		"\x1b[57421u":   {Code: PageUp},
		"\x1b[57422u":   {Code: PageDown},
		"\x1b[57423u":   {Code: Home},
		"\x1b[57424u":   {Code: End},
		"\x1b[57426u":   {Code: Delete},
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q: expected %+v, got %v", input, want, got)
		}
	}
}

func TestKeyboardProtocolKeypadKeysTypeTheirText(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1b[57399u":     {Code: Rune, Value: '0'},
		"\x1b[57404;129u": {Code: Rune, Value: '5'},
		"\x1b[57408u":     {Code: Rune, Value: '9'},
		"\x1b[57409u":     {Code: Rune, Value: '.'},
		"\x1b[57410u":     {Code: Rune, Value: '/'},
		"\x1b[57411u":     {Code: Rune, Value: '*'},
		"\x1b[57412u":     {Code: Rune, Value: '-'},
		"\x1b[57413u":     {Code: Rune, Value: '+'},
		"\x1b[57415u":     {Code: Rune, Value: '='},
		"\x1b[57416u":     {Code: Rune, Value: ','},
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q: expected %+v, got %v", input, want, got)
		}
	}
}

func TestKeyboardProtocolLockModifiersAreIgnored(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1b[127;129u": {Code: Backspace},
		"\x1b[13;65u":   {Code: Enter},
		"\x1b[13;194u":  {Code: Enter, Mod: Shift},
		"\x1b[99;133u":  {Code: Rune, Value: 'c', Mod: Ctrl},
		"\x1b[1;131A":   {Code: Up, Mod: Alt},
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q: expected %+v, got %v", input, want, got)
		}
	}
}

func TestUnsupportedKeyboardProtocolFunctionalKeysAreNotText(t *testing.T) {
	for _, input := range []string{
		"\x1b[57376u",
		"\x1b[57425u",
		"\x1b[57427u",
		"\x1b[57441;2u",
	} {
		got := decode(t, input)
		if len(got) != 1 || got[0] != (Key{Code: Unknown}) {
			t.Errorf("%q: expected an unknown key, got %v", input, got)
		}
	}
}

func TestLegacyAltPrefixesModifyTheFollowingKey(t *testing.T) {
	for input, want := range map[string]Key{
		"\x1ba":    {Code: Rune, Value: 'a', Mod: Alt},
		"\x1b\r":   {Code: Enter, Mod: Alt},
		"\x1b\b":   {Code: Backspace, Mod: Alt},
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

func TestApplicationCursorKeysAreRecognised(t *testing.T) {
	for input, want := range map[string]Code{
		"\x1bOA": Up,
		"\x1bOB": Down,
		"\x1bOC": Right,
		"\x1bOD": Left,
		"\x1bOH": Home,
		"\x1bOF": End,
		"\x1bOM": Enter,
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
		"\x1b[7~":   {Code: Home},
		"\x1b[8~":   {Code: End},
		"\x1b[1;5~": {Code: Home, Mod: Ctrl},
		"\x1b[4;5~": {Code: End, Mod: Ctrl},
		"\x1b[7;5~": {Code: Home, Mod: Ctrl},
		"\x1b[8;5~": {Code: End, Mod: Ctrl},
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
