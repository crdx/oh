package edit

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestUnescapeKeepsMalformedBackslashEscapes(t *testing.T) {
	for encodedText, want := range map[string]string{
		`path\`:     `path\`,
		`path\file`: `path\file`,
	} {
		if got := unescape(encodedText); got != want {
			t.Errorf("unescape(%q) = %q, want %q", encodedText, got, want)
		}
	}
}

func TestHistoryTurnsAwayBlankAndRepeatedEntries(t *testing.T) {
	self := NewHistory("", 0)

	for _, line := range []string{"one", "one", "", "   ", "\n", "two", "two", "one"} {
		self.Add(line)
	}

	if want := []string{"one", "two", "one"}; !slices.Equal(self.lines, want) {
		t.Errorf("got %q, want %q", self.lines, want)
	}
}

func TestHistoryKeepsOnlyTheMostRecentEntriesUpToItsLimit(t *testing.T) {
	self := NewHistory("", 2)

	for _, line := range []string{"one", "two", "three"} {
		self.Add(line)
	}

	if want := []string{"two", "three"}; !slices.Equal(self.lines, want) {
		t.Errorf("got %q, want %q", self.lines, want)
	}
}

func TestHistorySurvivesARoundTripThroughItsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "history")
	written := []string{"plain", "two\nlines", `a\backslash`, `an escaped \n`}

	self := NewHistory(path, 0)
	for _, line := range written {
		self.Add(line)
	}

	if got := NewHistory(path, 0).lines; !slices.Equal(got, written) {
		t.Errorf("read back %q, want %q", got, written)
	}
}

func TestHistoryStartsEmptyWhenThereIsNoFileToReadYet(t *testing.T) {
	self := NewHistory(filepath.Join(t.TempDir(), "absent"), 0)

	if len(self.lines) != 0 {
		t.Errorf("expected nothing recalled, got %q", self.lines)
	}
}

func TestRecallWalksBackThroughTheEntriesAndReturnsToWhatWasTyped(t *testing.T) {
	history := NewHistory("", 0)
	for _, line := range []string{"one", "two", "three"} {
		history.Add(line)
	}

	self := history.recall()

	for _, want := range []string{"three", "two", "one"} {
		if got, walked := self.Walk("typing", -1); !walked || got != want {
			t.Fatalf("walking back got %q (%t), want %q", got, walked, want)
		}
	}

	for _, want := range []string{"two", "three", "typing"} {
		if got, walked := self.Walk("ignored", 1); !walked || got != want {
			t.Fatalf("walking forward got %q (%t), want %q", got, walked, want)
		}
	}
}

func TestRecallStopsAtEitherEndOfTheEntries(t *testing.T) {
	history := NewHistory("", 0)
	history.Add("only")

	self := history.recall()

	if got, walked := self.Walk("", 1); walked {
		t.Errorf("expected nothing ahead of the newest entry, got %q", got)
	}

	if _, walked := self.Walk("", -1); !walked {
		t.Fatal("expected the one entry behind us")
	}

	if got, walked := self.Walk("", -1); walked {
		t.Errorf("expected nothing behind the oldest entry, got %q", got)
	}
}

func TestSearchNarrowsTheNewestMatchingHistoryEntryIncrementally(t *testing.T) {
	history := NewHistory("", 0)
	for _, line := range []string{"git status", "just test", "git diff"} {
		history.Add(line)
	}

	self := history.search("unfinished")
	self.add('g')
	self.add('i')
	self.add('t')

	if got := self.getText(); got != "git diff" {
		t.Errorf("got %q, want the newest matching entry", got)
	}
	if got := self.getQuery(); got != "git" {
		t.Errorf("got query %q, want %q", got, "git")
	}

	self.previous()
	if got := self.getText(); got != "git status" {
		t.Errorf("got %q, want the previous matching entry", got)
	}
}

func TestSearchBackspaceRestoresTheMatchForTheShorterQuery(t *testing.T) {
	history := NewHistory("", 0)
	history.Add("git status")
	history.Add("git diff")

	self := history.search("unfinished")
	for _, value := range "status" {
		self.add(value)
	}
	if got := self.getText(); got != "git status" {
		t.Fatalf("got %q, want the older narrow match", got)
	}

	for range len("status") {
		self.deleteBackward()
	}
	if got := self.getText(); got != "unfinished" {
		t.Errorf("got %q, want the original input", got)
	}
}

func TestSearchKeepsTheLastMatchWhenTheQueryFails(t *testing.T) {
	history := NewHistory("", 0)
	history.Add("git diff")

	self := history.search("unfinished")
	for _, value := range "git z" {
		self.add(value)
	}

	if got := self.getText(); got != "git diff" {
		t.Errorf("got %q, want the last matching entry", got)
	}
}
