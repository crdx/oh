package edit

import (
	"bufio"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/key"
)

func inputFromKeys(t *testing.T, text string) *Input {
	t.Helper()

	self := NewInput(nil)
	decoder := key.NewDecoder(bufio.NewReader(strings.NewReader(text)))

	for {
		keypress, err := decoder.Next()
		if err != nil {
			return self
		}

		self.Apply(keypress, false)
	}
}

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

func TestPastedTextArrivesWithoutWhatTheTerminalWouldObey(t *testing.T) {
	for name, pasted := range map[string]struct {
		text string
		want string
	}{
		"clear screen": {text: "ls\x1b[2J -la", want: "ls -la"},
		"clipboard":    {text: "hello \x1b]52;c;cHduZWQ=\x07world", want: "hello world"},
		"bell":         {text: "ding\x07", want: "ding"},
		"carriage":     {text: "one\r\ntwo\rthree", want: "one\ntwo\nthree"},
		"kept":         {text: "one\n\ttwo", want: "one\n\ttwo"},
	} {
		t.Run(name, func(t *testing.T) {
			input := NewInput(NewHistory("", 0))
			input.InsertPasted(pasted.text)

			if got := input.Text(); got != pasted.want {
				t.Errorf("got %q, want %q", got, pasted.want)
			}
		})
	}
}

func TestABracketedPasteDropsTheControlRunesItCarries(t *testing.T) {
	input := NewInput(NewHistory("", 0))

	input.Apply(key.Key{Code: key.PasteStart}, false)
	for _, character := range "ls\x1b[2J -la\x07" {
		input.Apply(key.Key{Code: key.Rune, Value: character}, false)
	}
	input.Apply(key.Key{Code: key.PasteEnd}, false)

	if got, want := input.Text(), "ls[2J -la"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPasteKeepsItsLineBreaks(t *testing.T) {
	for name, payload := range map[string]string{
		"lf":   "one\ntwo\nthree",
		"cr":   "one\rtwo\rthree",
		"crlf": "one\r\ntwo\r\nthree",
	} {
		self := inputFromKeys(t, pasteStart+payload+pasteEnd)

		if got := self.Text(); got != "one\ntwo\nthree" {
			t.Errorf("%s: expected three lines, got %q", name, got)
		}
	}
}

func TestAPasteOfMoreThanFiveLinesBecomesAFencedCodeBlock(t *testing.T) {
	for name, pasted := range map[string]struct {
		text string
		want string
	}{
		"prose is unlabelled": {
			text: "one\ntwo\nthree\nfour\nfive\nsix",
			want: "```\none\ntwo\nthree\nfour\nfive\nsix\n```",
		},
		"distinctive Go is labelled": {
			text: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
			want: "```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```",
		},
		"an inner fence makes the outer fence longer": {
			text: "one\n```\nthree\nfour\nfive\nsix",
			want: "````\none\n```\nthree\nfour\nfive\nsix\n````",
		},
	} {
		t.Run(name, func(t *testing.T) {
			self := NewInput(nil)
			self.InsertPasted(pasted.text)

			if got := self.Text(); got != pasted.want {
				t.Errorf("got %q, want %q", got, pasted.want)
			}
		})
	}
}

func TestABracketedPasteOfMoreThanFiveLinesBecomesAFencedCodeBlock(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"one\ntwo\nthree\nfour\nfive\nsix"+pasteEnd)

	want := "```\none\ntwo\nthree\nfour\nfive\nsix\n```"
	if got := self.Text(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPasteOfFiveLinesIsNotFenced(t *testing.T) {
	for name, pasted := range map[string]string{
		"without final newline": "one\ntwo\nthree\nfour\nfive",
		"with final newline":    "one\ntwo\nthree\nfour\nfive\n",
	} {
		t.Run(name, func(t *testing.T) {
			self := NewInput(nil)
			self.InsertPasted(pasted)

			if got := self.Text(); got != pasted {
				t.Errorf("got %q, want %q", got, pasted)
			}
		})
	}
}

func TestAFencedPasteStartsAndEndsOnItsOwnLines(t *testing.T) {
	self := inputFromKeys(t, "beforeafter")
	for range len("after") {
		self.Apply(key.Key{Code: key.Left}, false)
	}

	self.InsertPasted("one\ntwo\nthree\nfour\nfive\nsix")

	want := "before\n```\none\ntwo\nthree\nfour\nfive\nsix\n```\nafter"
	if got := self.Text(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOnlyDistinctivePastesNameALanguage(t *testing.T) {
	for name, pasted := range map[string]struct {
		text string
		want string
	}{
		"Go": {
			text: "package main\n\nfunc main() {}",
			want: "go",
		},
		"Python": {
			text: "def greet(name: str) -> str:\n    return f\"Hello, {name}\"",
			want: "python",
		},
		"bash shebang": {
			text: "#!/usr/bin/env bash\nset -euo pipefail",
			want: "bash",
		},
		"sh shebang": {
			text: "#!/bin/sh\nset -eu",
			want: "sh",
		},
		"Python shebang": {
			text: "#!/usr/bin/python3.14\nprint(\"hello\")",
			want: "python",
		},
		"Ruby shebang": {
			text: "#!/usr/bin/env ruby\nputs \"hello\"",
			want: "ruby",
		},
		"Perl shebang": {
			text: "#!/usr/bin/perl\nprint \"hello\\n\";",
			want: "perl",
		},
		"PHP shebang": {
			text: "#!/usr/bin/php\necho \"hello\";",
			want: "php",
		},
		"Lua shebang": {
			text: "#!/usr/bin/env lua\nprint(\"hello\")",
			want: "lua",
		},
		"goscript shebang": {
			text: "#!/usr/bin/env -S goscript run\npackage main",
			want: "go",
		},
		"JSON object": {
			text: "{\n  \"name\": \"oh\"\n}",
			want: "json",
		},
		"HTML doctype": {
			text: "<!DOCTYPE html>\n<html></html>",
			want: "html",
		},
		"XML declaration": {
			text: "<?xml version=\"1.0\"?>\n<message>hello</message>",
			want: "xml",
		},
		"PHP opening tag": {
			text: "<?php\necho \"hello\";",
			want: "php",
		},
		"Git diff": {
			text: "diff --git a/old b/new\n--- a/old\n+++ b/new",
			want: "diff",
		},
		"Dockerfile": {
			text: "FROM alpine:3.23\nRUN echo hello",
			want: "dockerfile",
		},
		"a JSON scalar is ambiguous": {
			text: "true",
		},
		"a Go package without a declaration is ambiguous": {
			text: "package delivery",
		},
		"Docker instructions after prose are ambiguous": {
			text: "build notes\nFROM alpine:3.23\nRUN echo hello",
		},
		"C and C++ are ambiguous": {
			text: "#include <stdio.h>\nint main(void) { return 0; }",
		},
		"JavaScript and TypeScript are ambiguous": {
			text: "const greet = (name) => {\n  console.log(name)\n}",
		},
		"TOML and INI are ambiguous": {
			text: "[server]\nport = 8080",
		},
		"YAML and plain mappings are ambiguous": {
			text: "name: oh\nversion: 1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pasteLanguage(pasted.text); got != pasted.want {
				t.Errorf("got %q, want %q", got, pasted.want)
			}
		})
	}
}

func TestAPastedTabBecomesSpaces(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"a\tb"+pasteEnd)

	if got := self.Text(); got != "a"+strings.Repeat(" ", tabStop)+"b" {
		t.Errorf("expected the tab to be spaces, got %q", got)
	}
}

func TestAPastePreservesRelativeIndentation(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"    one\n        two\n      three"+pasteEnd)

	if got := self.Text(); got != "one\n    two\n  three" {
		t.Errorf("expected the paste left-aligned with relative indentation preserved, got %q", got)
	}
}

func TestAnAlreadyLeftAlignedPasteKeepsItsIndentation(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"one\n    two"+pasteEnd)

	if got := self.Text(); got != "one\n    two" {
		t.Errorf("expected the paste unchanged, got %q", got)
	}
}

func TestAPasteEndsWhereTheTerminalSaysItDoes(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"one"+pasteEnd)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != AcceptInput {
		t.Errorf("expected the line to be finished after the paste, got %v", got)
	}

	if got := self.Text(); got != "one" {
		t.Errorf("expected the pasted text, got %q", got)
	}
}

func TestReturnOutsideAPasteFinishesTheLine(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != AcceptInput {
		t.Errorf("expected the line to be finished, got %v", got)
	}
}

func TestTwoReturnsOnAnEmptyIdleLineAskToContinue(t *testing.T) {
	self := NewInput(nil)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != DrawInput {
		t.Errorf("expected the first return to do nothing, got %v", got)
	}

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != ContinueTurn {
		t.Errorf("expected the second return to continue, got %v", got)
	}
}

func TestReturnAcceptsInputDuringARunningTurn(t *testing.T) {
	self := inputFromKeys(t, "hello")

	if got := self.Apply(key.Key{Code: key.Enter}, true); got != AcceptInput {
		t.Errorf("expected return to accept the input, got %v", got)
	}

	if self.Text() != "hello" {
		t.Errorf("expected what was typed to be left alone, got %q", self.Text())
	}
}

func TestRepeatedReturnsCoolOffSoOneLineIsAcceptedOnce(t *testing.T) {
	now := time.Time{}
	self := inputFromKeys(t, "/unknown")
	self.currentTime = func() time.Time { return now }

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != AcceptInput {
		t.Fatalf("expected the first return to accept the input, got %v", got)
	}

	for range 2 {
		now = now.Add(acceptCoolOff * 9 / 10)
		if got := self.Apply(key.Key{Code: key.Enter}, false); got != DrawInput {
			t.Errorf("expected a repeated return to extend the cool-off, got %v", got)
		}
	}

	now = now.Add(acceptCoolOff)
	if got := self.Apply(key.Key{Code: key.Enter}, false); got != AcceptInput {
		t.Errorf("expected return to accept the input after the cool-off, got %v", got)
	}
}

func TestTwoReturnsOnAnEmptyRunningLineAskToContinue(t *testing.T) {
	for name, inputText := range map[string]string{"empty": "", "whitespace": " "} {
		self := inputFromKeys(t, inputText)

		if got := self.Apply(key.Key{Code: key.Enter}, true); got != DrawInput {
			t.Errorf("%s: expected the first return to leave the turn running, got %v", name, got)
		}

		if got := self.Apply(key.Key{Code: key.Enter}, true); got != ContinueTurn {
			t.Errorf("%s: expected the second return to continue, got %v", name, got)
		}
	}
}

func TestDoubleReturnHasACoolOffBeforeItCanContinueAgain(t *testing.T) {
	now := time.Time{}
	self := NewInput(nil)
	self.currentTime = func() time.Time { return now }

	self.Apply(key.Key{Code: key.Enter}, true)
	if got := self.Apply(key.Key{Code: key.Enter}, true); got != ContinueTurn {
		t.Fatalf("expected the first double return to continue, got %v", got)
	}

	self.Reset()
	for range 2 {
		now = now.Add(continueCoolOff * 9 / 10)
		if got := self.Apply(key.Key{Code: key.Enter}, true); got != DrawInput {
			t.Errorf("expected a held return to extend the cool-off, got %v", got)
		}
	}

	now = now.Add(continueCoolOff)
	if got := self.Apply(key.Key{Code: key.Enter}, true); got != DrawInput {
		t.Errorf("expected the first return after the cool-off to do nothing, got %v", got)
	}
	if got := self.Apply(key.Key{Code: key.Enter}, true); got != ContinueTurn {
		t.Errorf("expected a new double return after the cool-off to continue, got %v", got)
	}
}

func TestReturnsMustBeConsecutiveToContinueAnEmptyRunningTurn(t *testing.T) {
	self := NewInput(nil)
	self.Apply(key.Key{Code: key.Enter}, true)
	self.Apply(key.Key{Code: key.Left}, true)

	if got := self.Apply(key.Key{Code: key.Enter}, true); got != DrawInput {
		t.Errorf("expected an intervening key to clear the first return, got %v", got)
	}
}

func TestPendingReturnDoesNotSurviveATurnStateChange(t *testing.T) {
	self := NewInput(nil)
	self.Apply(key.Key{Code: key.Enter}, true)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != DrawInput {
		t.Errorf("expected the first idle return to do nothing, got %v", got)
	}
}

func TestShiftReturnOpensALine(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'a'}, true)
	self.Apply(key.Key{Code: key.Enter, Mod: key.Shift}, true)
	self.Apply(key.Key{Code: key.Rune, Value: 'b'}, true)

	if got := self.Text(); got != "a\nb" {
		t.Errorf("expected two lines, got %q", got)
	}
}

func TestWideCharactersWrapByTheCellsTheyTake(t *testing.T) {
	self := NewInput(nil)

	for _, value := range "日本語" {
		self.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	frame := self.Frame(5)
	rows, cursorRow, cursorColumn := frame.Rows, frame.Row, frame.Column

	if len(rows) != 2 || rows[0] != "日本" || rows[1] != "語" {
		t.Errorf("expected two characters then one, got %q", rows)
	}

	if cursorRow != 1 || cursorColumn != 2 {
		t.Errorf("expected the cursor after the third character, got row %d column %d", cursorRow, cursorColumn)
	}
}

func TestInputWrapsBetweenWords(t *testing.T) {
	frame := inputFromKeys(t, "one two three").Frame(7)
	want := []string{"one two", "three"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected words kept together, got %q", frame.Rows)
	}
}

func TestAWrappingSpaceDoesNotStartTheNextRow(t *testing.T) {
	frame := inputFromKeys(t, "one two").Frame(4)
	want := []string{"one", "two"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected the wrapping space hidden, got %q", frame.Rows)
	}
}

func TestAWordWiderThanTheInputStillWraps(t *testing.T) {
	frame := inputFromKeys(t, "abcdef").Frame(4)
	want := []string{"abcd", "ef"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected the long word to wrap, got %q", frame.Rows)
	}
}

func TestVerticalMovementUsesWrappedRowsBeforeHistory(t *testing.T) {
	history := NewHistory("", 0)
	history.Add("earlier message")
	self := NewInput(history)
	self.SetText("one two three")
	self.Frame(7)

	self.Apply(key.Key{Code: key.Up}, false)
	frame := self.Frame(7)

	if got := self.Text(); got != "one two three" {
		t.Errorf("expected the draft to stay in place, got %q", got)
	}
	if frame.Row != 0 || frame.Column != 5 {
		t.Errorf("expected the cursor at 0,5, got %d,%d", frame.Row, frame.Column)
	}

	self.Apply(key.Key{Code: key.Down}, false)
	frame = self.Frame(7)
	if frame.Row != 1 || frame.Column != 5 {
		t.Errorf("expected the cursor back at 1,5, got %d,%d", frame.Row, frame.Column)
	}

	self.Apply(key.Key{Code: key.Up}, false)
	self.Apply(key.Key{Code: key.Up}, false)
	if got := self.Text(); got != "earlier message" {
		t.Errorf("expected history after moving past the first row, got %q", got)
	}
}

func TestVerticalMovementUsesDisplayColumnsForWideCharacters(t *testing.T) {
	self := NewInput(nil)
	self.SetText("日本ab")
	self.Frame(4)

	self.Apply(key.Key{Code: key.Up}, false)
	frame := self.Frame(4)
	if frame.Row != 0 || frame.Column != 2 {
		t.Errorf("expected the cursor after one wide character, got %d,%d", frame.Row, frame.Column)
	}

	self.Apply(key.Key{Code: key.Down}, false)
	frame = self.Frame(4)
	if frame.Row != 1 || frame.Column != 2 {
		t.Errorf("expected the cursor back at the end, got %d,%d", frame.Row, frame.Column)
	}
}

func TestVerticalMovementIncludesTheTrailingCursorRow(t *testing.T) {
	self := NewInput(nil)
	self.SetText("full")
	self.Frame(4)

	self.Apply(key.Key{Code: key.Up}, false)
	frame := self.Frame(4)
	if frame.Row != 0 || frame.Column != 0 {
		t.Errorf("expected the cursor at the start of the full row, got %d,%d", frame.Row, frame.Column)
	}

	self.Apply(key.Key{Code: key.Down}, false)
	frame = self.Frame(4)
	if frame.Row != 1 || frame.Column != 0 {
		t.Errorf("expected the cursor back on the trailing row, got %d,%d", frame.Row, frame.Column)
	}
}

func TestLeadingWhitespaceKeepsItsRoom(t *testing.T) {
	frame := (&Input{buffer: &Buffer{runes: []rune("    one")}}).Frame(3)
	want := []string{"   ", "one"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected leading whitespace preserved, got %q", frame.Rows)
	}
}

func TestAnOverlongWordStartsItsOwnRow(t *testing.T) {
	frame := (&Input{buffer: &Buffer{runes: []rune("one abcdef")}}).Frame(5)
	want := []string{"one", "abcde", "f"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected the word to start a row before the hard wrap, got %q", frame.Rows)
	}
}

func TestTheCursorCrossesAHiddenWrappingSpace(t *testing.T) {
	for cursor := 3; cursor <= 4; cursor++ {
		frame := (&Input{buffer: &Buffer{runes: []rune("one two"), cursor: cursor}}).Frame(4)

		if frame.Row != 1 || frame.Column != 0 {
			t.Errorf("at %d expected the next row, got %d,%d", cursor, frame.Row, frame.Column)
		}
	}
}

func lines(count int) *Input {
	self := NewInput(nil)

	for number := range count {
		if number > 0 {
			self.Apply(key.Key{Code: key.Enter, Mod: key.Shift}, false)
		}

		self.Apply(key.Key{Code: key.Rune, Value: rune('a' + number%26)}, false)
	}

	return self
}

func TestWhatIsOutOfSightIsCountedAtBothEdges(t *testing.T) {
	self := lines(maxRows * 3)

	for range maxRows {
		self.Apply(key.Key{Code: key.Up}, false)
	}

	frame := self.Frame(80)

	if frame.HiddenLinesAbove == 0 || frame.HiddenLinesBelow == 0 {
		t.Errorf("expected rows out of sight both ways, got %d above and %d below",
			frame.HiddenLinesAbove, frame.HiddenLinesBelow)
	}

	if total := frame.HiddenLinesAbove + len(frame.Rows) + frame.HiddenLinesBelow; total != maxRows*3 {
		t.Errorf("expected the counts and the rows drawn to be the whole line, got %d", total)
	}
}

func TestNothingIsOutOfSightWhereTheLineFits(t *testing.T) {
	frame := lines(maxRows).Frame(80)

	if frame.HiddenLinesAbove != 0 || frame.HiddenLinesBelow != 0 {
		t.Errorf("expected nothing out of sight, got %d above and %d below",
			frame.HiddenLinesAbove, frame.HiddenLinesBelow)
	}
}

func TestAShortLineIsDrawnWhole(t *testing.T) {
	rows := lines(maxRows).Frame(80).Rows

	if len(rows) != maxRows {
		t.Errorf("expected all %d rows, got %d", maxRows, len(rows))
	}
}

func TestATallLineIsCutToTheTallestTheInputMayDraw(t *testing.T) {
	rows := lines(maxRows * 3).Frame(80).Rows

	if len(rows) != maxRows {
		t.Errorf("expected %d rows, got %d", maxRows, len(rows))
	}
}

func TestTheCursorStaysWithinTheRowsDrawn(t *testing.T) {
	self := lines(maxRows * 3)

	for moveCount := range maxRows * 3 {
		frame := self.Frame(80)
		rows, cursorRow := frame.Rows, frame.Row

		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Fatalf("after %d moves the cursor sat at row %d of %d", moveCount, cursorRow, len(rows))
		}

		self.Apply(key.Key{Code: key.Up}, false)
	}
}

func TestTheEndOfATallLineIsWhatIsDrawn(t *testing.T) {
	frame := lines(maxRows + 2).Frame(80)
	rows, cursorRow := frame.Rows, frame.Row

	if want := string(rune('a' + (maxRows+1)%26)); rows[len(rows)-1] != want {
		t.Errorf("expected the last row to be %q, got %q", want, rows[len(rows)-1])
	}

	if cursorRow != maxRows-1 {
		t.Errorf("expected the cursor on the last row, got %d", cursorRow)
	}
}

func TestTheStartOfATallLineIsDrawnWhenTheCursorIsThere(t *testing.T) {
	self := lines(maxRows * 2)

	for range maxRows * 2 {
		self.Apply(key.Key{Code: key.Up}, false)
	}

	frame := self.Frame(80)
	rows, cursorRow := frame.Rows, frame.Row

	if rows[0] != "a" || cursorRow != 0 {
		t.Errorf("expected the first row drawn with the cursor on it, got %q at %d", rows[0], cursorRow)
	}
}

func TestACharacterWiderThanTheLineStaysWhereItIs(t *testing.T) {
	rows, cursorRow, cursorColumn := layout(&Buffer{runes: []rune("日"), cursor: 1}, 1)

	if len(rows) != 2 || rows[0] != "日" {
		t.Errorf("expected the character on the first row, got %q", rows)
	}

	if cursorRow != 1 || cursorColumn != 0 {
		t.Errorf("expected the cursor on the next row, got %d,%d", cursorRow, cursorColumn)
	}
}

var controlC = key.Key{Code: key.Rune, Value: 'c', Mod: key.Ctrl}

func TestReadlineControlBindingsEditTheCurrentLine(t *testing.T) {
	for name, test := range map[string]struct {
		letter rune
		before string
		want   string
	}{
		"beginning":     {letter: 'a', before: "one\nt|wo", want: "one\n|two"},
		"backward":      {letter: 'b', before: "one\nt|wo", want: "one\n|two"},
		"end":           {letter: 'e', before: "one\nt|wo", want: "one\ntwo|"},
		"forward":       {letter: 'f', before: "one\nt|wo", want: "one\ntw|o"},
		"kill line":     {letter: 'k', before: "one\nt|wo", want: "one\nt|"},
		"backward word": {letter: 'w', before: "one two|", want: "one |"},
	} {
		self := NewInput(nil)
		self.buffer = bufferFrom(t, test.before)

		self.Apply(key.Key{Code: key.Rune, Value: test.letter, Mod: key.Ctrl}, false)

		if got := markCursor(self.buffer); got != test.want {
			t.Errorf("%s: got %q, want %q", name, got, test.want)
		}
	}
}

func TestControlRSearchesHistoryIncrementallyAndRepeatsBackwards(t *testing.T) {
	history := NewHistory("", 0)
	for _, line := range []string{"git status", "just test", "git diff"} {
		history.Add(line)
	}
	self := NewInput(history)
	controlR := key.Key{Code: key.Rune, Value: 'r', Mod: key.Ctrl}

	self.Apply(controlR, false)
	if frame := self.Frame(80); !frame.IsSearching || frame.SearchQuery != "" {
		t.Fatalf("ctrl+r did not start an empty search: %+v", frame)
	}

	for _, value := range "git" {
		self.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}
	if got := self.Text(); got != "git diff" {
		t.Fatalf("got %q, want the newest match", got)
	}

	self.Apply(controlR, false)
	if got := self.Text(); got != "git status" {
		t.Errorf("got %q, want the previous match", got)
	}

	self.Apply(key.Key{Code: key.Escape}, false)
	if frame := self.Frame(80); frame.IsSearching {
		t.Error("escape did not finish the search")
	}
	if got := self.Text(); got != "git status" {
		t.Errorf("finishing the search changed the match to %q", got)
	}
}

func TestControlUAlwaysClearsTheInput(t *testing.T) {
	self := inputFromKeys(t, "hello")
	self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false)
	self.Apply(key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl}, false)

	if self.Text() != "" || self.IsPrefixPending() {
		t.Errorf("ctrl+u left input %q with pending=%v", self.Text(), self.IsPrefixPending())
	}
}

func TestOneControlCLeavesAWrittenLineAlone(t *testing.T) {
	self := inputFromKeys(t, "hello")

	if got := self.Apply(controlC, false); got != DrawInput {
		t.Errorf("expected the first ctrl+c to draw, got %v", got)
	}

	if self.Text() != "hello" {
		t.Errorf("expected the first ctrl+c to keep the line, got %q", self.Text())
	}
}

func TestTwoControlCsClearTheInput(t *testing.T) {
	self := inputFromKeys(t, "hello")
	self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false)
	self.Apply(controlC, false)
	self.Apply(controlC, false)

	if self.Text() != "" || self.IsPrefixPending() {
		t.Errorf("ctrl+c twice left input %q with pending=%v", self.Text(), self.IsPrefixPending())
	}
}

func TestAKeyBetweenTwoControlCsKeepsTheLine(t *testing.T) {
	self := inputFromKeys(t, "hello")
	self.Apply(controlC, false)
	self.Apply(key.Key{Code: key.Rune, Value: '!'}, false)
	self.Apply(controlC, false)

	if self.Text() != "hello!" {
		t.Errorf("expected the disarmed ctrl+c to keep the line, got %q", self.Text())
	}
}

func TestControlCOnAnEmptyLineClearsAtOnce(t *testing.T) {
	self := NewInput(nil)
	self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false)

	if got := self.Apply(controlC, false); got != DrawInput {
		t.Errorf("expected ctrl+c on an empty line to draw, got %v", got)
	}

	if self.IsPrefixPending() {
		t.Error("expected ctrl+c on an empty line to drop the pending prefix")
	}
}

func TestThePrefixAndALetterAskForOneSwap(t *testing.T) {
	for letter, want := range map[rune]Action{
		'w': ToggleWrite,
		'x': ToggleShell,
		'n': ToggleNetwork,
		'g': ToggleGit,
		'l': ToggleLookup,
	} {
		self := NewInput(nil)

		self.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

		if got := self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false); got != DrawInput {
			t.Errorf("ctrl+x: expected the prefix to swap nothing on its own, got %v", got)
		}

		if got := self.Apply(key.Key{Code: key.Rune, Value: letter}, false); got != want {
			t.Errorf("ctrl+x %c: expected %v, got %v", letter, want, got)
		}

		if got := self.Text(); got != "a" {
			t.Errorf("ctrl+x %c: expected the line to be left alone, got %q", letter, got)
		}
	}
}

func TestALetterNamingNoModeIsSwallowed(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false)

	if got := self.Apply(key.Key{Code: key.Rune, Value: 'q'}, false); got != DrawInput {
		t.Errorf("expected nothing to be asked for, got %v", got)
	}

	if got := self.Text(); got != "" {
		t.Errorf("expected the letter to be swallowed, got %q", got)
	}

	self.Apply(key.Key{Code: key.Rune, Value: 'w'}, false)

	if got := self.Text(); got != "w" {
		t.Errorf("expected the line to carry on as text, got %q", got)
	}
}

func TestAPastedTextIsInsertedAtTheCursorWithItsIndentationNormalised(t *testing.T) {
	self := inputFromKeys(t, "beforeafter")
	for range len("after") {
		self.Apply(key.Key{Code: key.Left}, false)
	}

	self.InsertPasted("    if isReady {\n        begin()\n    }")

	want := "beforeif isReady {\n    begin()\n}after"
	if got := self.Text(); got != want {
		t.Errorf("pasted text is %q, want %q", got, want)
	}
}

func TestControlVIsNoLongerBoundNowThatTheTerminalReportsPastesItself(t *testing.T) {
	self := inputFromKeys(t, "draft")

	if got := self.Apply(key.Key{Code: key.Rune, Value: 'v', Mod: key.Ctrl}, false); got != DrawInput {
		t.Errorf("ctrl+v returned %v, want a redraw and nothing else", got)
	}

	if got := self.Text(); got != "draft" {
		t.Errorf("ctrl+v changed the line to %q", got)
	}
}

func TestControlDStopsARunningTurnWhateverIsTyped(t *testing.T) {
	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	for name, inputText := range map[string]string{"empty": "", "typed": "hello"} {
		self := inputFromKeys(t, inputText)

		if got := self.Apply(keypress, true); got != CancelTurn {
			t.Errorf("%s: expected the turn to be cancelled, got %v", name, got)
		}

		if self.Text() != inputText {
			t.Errorf("%s: expected what was typed to be left alone, got %q", name, self.Text())
		}
	}
}

func TestControlDAtRestLeavesOnlyFromAnEmptyLine(t *testing.T) {
	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	if got := inputFromKeys(t, "").Apply(keypress, false); got != QuitSession {
		t.Errorf("expected an empty line to be the way out, got %v", got)
	}

	self := inputFromKeys(t, "hello")
	self.Apply(key.Key{Code: key.Home}, false)

	if got := self.Apply(keypress, false); got == QuitSession {
		t.Errorf("expected a line with something on it to keep the harness, got %v", got)
	}

	if self.Text() != "hello" {
		t.Errorf("expected the line to be untouched, got %q", self.Text())
	}
}

func TestTabInOrdinaryInputBecomesSpaces(t *testing.T) {
	self := inputFromKeys(t, "one")

	if got := self.Apply(key.Key{Code: key.Rune, Value: '\t'}, false); got != DrawInput {
		t.Errorf("got action %v", got)
	}
	if got := self.Text(); got != "one"+strings.Repeat(" ", tabStop) {
		t.Errorf("got text %q", got)
	}
}

func TestTabRequestsCompletionWithoutChangingTheInput(t *testing.T) {
	self := inputFromKeys(t, "/co")

	if got := self.Apply(key.Key{Code: key.Rune, Value: '\t'}, false); got != CompleteCommand {
		t.Errorf("got action %v", got)
	}
	if got := self.Text(); got != "/co" {
		t.Errorf("got text %q", got)
	}
}

func TestAltReturnForceAcceptsNonEmptyInput(t *testing.T) {
	self := inputFromKeys(t, "/unknown")

	if got := self.Apply(key.Key{Code: key.Enter, Mod: key.Alt}, false); got != ForceAcceptInput {
		t.Errorf("got action %v", got)
	}
}
