package edit

import (
	"slices"
	"testing"
)

const cursorMark = '|'

func bufferFrom(t *testing.T, marked string) *Buffer {
	t.Helper()

	runes := []rune(marked)

	cursor := slices.Index(runes, cursorMark)
	if cursor < 0 {
		t.Fatalf("no %q marking the cursor in %q", cursorMark, marked)
	}

	return &Buffer{runes: slices.Delete(runes, cursor, cursor+1), cursor: cursor}
}

func markCursor(self *Buffer) string {
	return string(slices.Insert(slices.Clone(self.runes), self.cursor, cursorMark))
}

func moves(t *testing.T, move func(*Buffer), cases map[string]string) {
	t.Helper()

	for before, want := range cases {
		self := bufferFrom(t, before)
		move(self)

		if got := markCursor(self); got != want {
			t.Errorf("from %q got %q, want %q", before, got, want)
		}
	}
}

func TestSettingTheTextLeavesTheCursorAtTheEnd(t *testing.T) {
	var self Buffer

	self.Set("one two")

	if got := markCursor(&self); got != "one two|" {
		t.Errorf("expected the cursor at the end, got %q", got)
	}
}

func TestTheLengthCountsCharactersRatherThanBytes(t *testing.T) {
	var self Buffer

	self.Set("héllo 日本")

	if got := self.Len(); got != 8 {
		t.Errorf("Len() = %d, want 8", got)
	}

	if got := len(self.Runes()); got != 8 {
		t.Errorf("len(Runes()) = %d, want 8", got)
	}

	if got := self.String(); got != "héllo 日本" {
		t.Errorf("String() = %q, want the text back unchanged", got)
	}
}

func TestInsertingPutsTheTextInAtTheCursorAndFollowsIt(t *testing.T) {
	moves(t, func(self *Buffer) { self.Insert([]rune("XY")) }, map[string]string{
		"|one":    "XY|one",
		"on|e":    "onXY|e",
		"one|":    "oneXY|",
		"|":       "XY|",
		"日|本":     "日XY|本",
		"one\n|t": "one\nXY|t",
	})
}

func TestMovingLeftAndRightStopsAtEitherEnd(t *testing.T) {
	moves(t, (*Buffer).MoveLeft, map[string]string{
		"|one": "|one",
		"o|ne": "|one",
		"one|": "on|e",
	})

	moves(t, (*Buffer).MoveRight, map[string]string{
		"|one": "o|ne",
		"on|e": "one|",
		"one|": "one|",
	})
}

func TestHomeAndEndStayOnTheLineTheCursorIsOn(t *testing.T) {
	moves(t, (*Buffer).MoveHome, map[string]string{
		"one\ntw|o":     "one\n|two",
		"one\n|two":     "one\n|two",
		"o|ne\ntwo":     "|one\ntwo",
		"one\n|\nthree": "one\n|\nthree",
	})

	moves(t, (*Buffer).MoveEnd, map[string]string{
		"one\nt|wo":     "one\ntwo|",
		"one\ntwo|":     "one\ntwo|",
		"o|ne\ntwo":     "one|\ntwo",
		"one\n|\nthree": "one\n|\nthree",
	})
}

func TestWordMotionCrossesThePunctuationBeforeTheWord(t *testing.T) {
	moves(t, (*Buffer).MoveWordLeft, map[string]string{
		"one two|":    "one |two",
		"one two |":   "one |two ",
		"one tw|o":    "one |two",
		"|one":        "|one",
		"one, two|":   "one, |two",
		"one\ntwo|":   "one\n|two",
		"snake_case|": "snake_|case",
		"count 42|":   "count |42",
		"  leading|":  "  |leading",
	})

	moves(t, (*Buffer).MoveWordRight, map[string]string{
		"|one two":    "one| two",
		"one| two":    "one two|",
		"|  leading":  "  leading|",
		"one two|":    "one two|",
		"|one\ntwo":   "one|\ntwo",
		"|snake_case": "snake|_case",
	})
}

func TestMovingUpAndDownRefusesToLeaveTheText(t *testing.T) {
	for name, text := range map[string]string{
		"one line":         "on|e",
		"empty":            "|",
		"first of several": "o|ne\ntwo",
	} {
		if self := bufferFrom(t, text); self.MoveUp() {
			t.Errorf("%s: expected no room above %q", name, text)
		}
	}

	for name, text := range map[string]string{
		"one line":        "on|e",
		"empty":           "|",
		"last of several": "one\nt|wo",
	} {
		if self := bufferFrom(t, text); self.MoveDown() {
			t.Errorf("%s: expected no room below %q", name, text)
		}
	}
}

func TestMovingUpAndDownKeepsTheColumnWhereTheLineIsLongEnough(t *testing.T) {
	moves(t, func(self *Buffer) { self.MoveUp() }, map[string]string{
		"abcd\nab|cd":      "ab|cd\nabcd",
		"abcd\n|abcd":      "|abcd\nabcd",
		"abcd\nabcd|":      "abcd|\nabcd",
		"ab\nabcd|":        "ab|\nabcd",
		"\nabcd|":          "|\nabcd",
		"one\ntwo\nth|ree": "one\ntw|o\nthree",
	})

	moves(t, func(self *Buffer) { self.MoveDown() }, map[string]string{
		"ab|cd\nabcd": "abcd\nab|cd",
		"|abcd\nabcd": "abcd\n|abcd",
		"abcd|\nabcd": "abcd\nabcd|",
		"abcd|\nab":   "abcd\nab|",
		"abcd|\n":     "abcd\n|",
	})
}

func TestDeletingBackwardAndForwardStopsAtEitherEnd(t *testing.T) {
	moves(t, (*Buffer).DeleteBackward, map[string]string{
		"|one": "|one",
		"o|ne": "|ne",
		"one|": "on|",
		"日本|":  "日|",
	})

	moves(t, (*Buffer).DeleteForward, map[string]string{
		"|one": "|ne",
		"on|e": "on|",
		"one|": "one|",
		"|日本":  "|本",
	})
}

func TestDeletingAWordBackwardLeavesTheCursorWhereTheWordBegan(t *testing.T) {
	moves(t, (*Buffer).DeleteWordBackward, map[string]string{
		"one two|":    "one |",
		"one two |":   "one |",
		"one tw|o":    "one |o",
		"one, two|":   "one, |",
		"|one":        "|one",
		"  |":         "|",
		"one\ntwo|":   "one\n|",
		"snake_case|": "snake_|",
	})
}

func TestDeletingAWhitespaceWordBackwardKeepsPunctuationWithItsWord(t *testing.T) {
	moves(t, (*Buffer).DeleteWhitespaceWordBackward, map[string]string{
		"one --two|":  "one |",
		"one --two |": "one |",
		"one --tw|o":  "one |o",
		"|one":        "|one",
		"one\ntwo|":   "one\n|",
	})
}

func TestDeletingToTheEndStopsAtTheEndOfTheCurrentLine(t *testing.T) {
	moves(t, (*Buffer).DeleteToEnd, map[string]string{
		"|one":        "|",
		"o|ne":        "o|",
		"one|":        "one|",
		"one\nt|wo":   "one\nt|",
		"o|ne\ntwo":   "o|\ntwo",
		"one\n|\ntwo": "one\n|\ntwo",
	})
}
