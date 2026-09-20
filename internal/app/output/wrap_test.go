package output

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
)

func TestTheConversationWrapsByCellsRatherThanCharacters(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 5}

	if got := screen.fit("日本語"); got != "日本\r\n語" {
		t.Errorf("expected a break after the second character, got %q", got)
	}

	if screen.column != 2 {
		t.Errorf("expected the cursor two cells along, got %d", screen.column)
	}
}

func TestTextPresentationEmojiDoesNotCreateAPhantomRow(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 8}

	if got := screen.fit(" test 🖊 \n"); got != " test 🖊 \r\n" {
		t.Errorf("expected the message to remain on one row, got %q", got)
	}

	if screen.openedRows != 1 {
		t.Errorf("expected one opened row, got %d", screen.openedRows)
	}
}

func TestJoinedEmojiAreMeasuredAsOneGrapheme(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 3}

	if got := screen.fit("👨‍👩‍👧x"); got != "👨‍👩‍👧x" {
		t.Errorf("expected the joined emoji to remain on one row, got %q", got)
	}
}

func TestAnEscapeSequenceTakesNoRoomOnTheRow(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 5}

	if got := screen.fit("\x1b[31mabcde\x1b[0m"); got != "\x1b[31mabcde\x1b[0m" {
		t.Errorf("expected the colour not to count against the width, got %q", got)
	}
}

func TestSizedTextTakesItsDeclaredRoomOnTheRow(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 5}
	fish := "\x1b]66;s=2:n=3:d=4:w=2;🐟\x1b\\"

	if got := screen.fit("a " + fish); got != "a \r\n"+fish {
		t.Errorf("expected room to be made for the fish, got %q", got)
	}
	if screen.column != 4 {
		t.Errorf("expected the cursor four cells along, got %d", screen.column)
	}
}

func TestCursorMovementTakesItsDeclaredRoomOnTheRow(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 10}
	text := "\x1b[4C  x"

	if got := screen.fit(text); got != text {
		t.Errorf("fit() = %q, want %q", got, text)
	}
	if screen.column != 7 {
		t.Errorf("expected the cursor seven cells along, got %d", screen.column)
	}
}

func FuzzFittingTerminalTextKeepsAValidCursor(fuzzer *testing.F) {
	for _, text := range []string{
		"plain words",
		"日本語",
		"\x1b[31mred\x1b[0m",
		"\x1b[4Cright",
		"\x1b[999999999999999999999Cright",
		"\x1b]66;s=2:w=2;🐟\x1b\\ after",
		"\x1b]66;s=-1:w=999999999999999999999;x",
	} {
		fuzzer.Add(text, uint8(20))
	}

	fuzzer.Fuzz(func(t *testing.T, text string, rawColumns uint8) {
		columns := int(rawColumns%80) + 1
		screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: columns}
		_ = screen.fit(text)

		if screen.column < 0 || screen.column > columns {
			t.Fatalf("fit(%q) left the cursor at %d of %d columns", text, screen.column, columns)
		}
		if screen.openedRows < 0 {
			t.Fatalf("fit(%q) opened %d rows", text, screen.openedRows)
		}
	})
}

func TestASpaceAtTheRowEdgeIsDroppedRatherThanCarried(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 5}

	if got := screen.fit("abcde fgh"); got != "abcde\r\nfgh" {
		t.Errorf("expected the break to eat the space, got %q", got)
	}

	if screen.column != 3 {
		t.Errorf("expected the cursor three cells along, got %d", screen.column)
	}

	if screen.openedRows != 1 {
		t.Errorf("expected one opened row, got %d", screen.openedRows)
	}
}

func TestEverySpaceAtTheRowEdgeIsDropped(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 5}

	if got := screen.fit("abcde   fgh"); got != "abcde\r\nfgh" {
		t.Errorf("expected the break to eat every space, got %q", got)
	}
}

func TestASpaceAtTheRowEdgeOpensNoRowOfItsOwn(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 5}

	if got := screen.fit("abcde \nx"); got != "abcde\r\nx" {
		t.Errorf("expected the newline to open the only new row, got %q", got)
	}

	if screen.openedRows != 1 {
		t.Errorf("expected one opened row, got %d", screen.openedRows)
	}
}

func TestAStyledFailureBreakingOnItsLastSpaceLeavesNoLeadingSpace(t *testing.T) {
	const columns = 61

	failure := style.Failure(
		`Post "https://api.anthropic.com/v1/messages": read tcp ` +
			`10.0.0.2:52134->160.79.104.10:443: read: software caused connection abort`,
	)

	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: columns}

	for row := range strings.SplitSeq(screen.fit(failure), "\r\n") {
		if strings.HasPrefix(style.Plain(row), " ") {
			t.Errorf("expected no row to begin with a space, got %q", row)
		}
	}
}

func TestACharacterWiderThanTheTerminalStaysWhereItIs(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 1}

	if got := screen.fit("日"); got != "日" {
		t.Errorf("expected no room to be made for it, got %q", got)
	}

	if screen.column != 1 {
		t.Errorf("expected the column held at the edge, got %d", screen.column)
	}
}
