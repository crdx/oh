package table

import (
	"strings"
	"testing"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
)

func fixedTable() *Table {
	return New(
		Column{Title: "Agent", Width: 10},
		Column{Title: "Title", IsFlex: true},
		Column{Title: "Model", Width: 8, MinRoom: 40},
		Column{Title: "Messages", Width: 4, Align: Right},
	)
}

func TestAFlexColumnTakesWhatTheOthersLeave(t *testing.T) {
	got := fixedTable().Row([]string{"otter", "a title", "codex", "12"}, 40)
	want := "otter       a title       codex       12"

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(got) != 40 {
		t.Errorf("got %d cells, want 40", len(got))
	}
}

func TestAColumnIsLeftOutOfANarrowTerminal(t *testing.T) {
	got := fixedTable().Row([]string{"otter", "a title", "codex", "12"}, 39)

	if strings.Contains(got, "codex") {
		t.Errorf("expected the model to be left out, got %q", got)
	}
	if !strings.Contains(got, "12") {
		t.Errorf("expected the messages to stay, got %q", got)
	}
}

func TestTheLeadingColumnsAreClippedRatherThanTheTrailingOnes(t *testing.T) {
	got := fixedTable().Row([]string{"otter", "a very long title indeed", "codex", "12"}, 24)

	if !strings.HasSuffix(got, "  12") {
		t.Errorf("expected the trailing column to survive, got %q", got)
	}
	if !strings.Contains(got, width.Ellipsis) {
		t.Errorf("expected the leading columns to be elided, got %q", got)
	}
	if style.Width(got) > 24 {
		t.Errorf("got %d cells, want no more than 24", style.Width(got))
	}
}

func TestARowIsNeverPaddedPastItsLastColumn(t *testing.T) {
	got := New(Column{Title: "Agent", Width: 10}, Column{Title: "Title", Width: 20}).
		Row([]string{"otter", "a title"}, 0)

	if strings.HasSuffix(got, " ") {
		t.Errorf("expected no trailing padding, got %q", got)
	}
	if got != "otter       a title" {
		t.Errorf("got %q", got)
	}
}

func TestColumnsWithoutAWidthAreSizedToTheirContent(t *testing.T) {
	rows := [][]string{
		{"running", "otter"},
		{"ended", "a-much-longer-name"},
	}

	sized := New(Column{Title: "Status"}, Column{Title: "Agent"}).Fit(rows)

	if got := sized.Header(0); got != "Status   Agent" {
		t.Errorf("got header %q", got)
	}
	if got := sized.Row(rows[0], 0); got != "running  otter" {
		t.Errorf("got %q", got)
	}
	if got := sized.Row(rows[1], 0); got != "ended    a-much-longer-name" {
		t.Errorf("got %q", got)
	}
}

func TestAColumnStyleIsAppliedToItsCellAlone(t *testing.T) {
	styled := New(
		Column{Title: "Agent", Width: 10},
		Column{Title: "Identifier", Width: 12, Style: style.Subtle},
	)

	got := styled.Row([]string{"otter", "gpt-5.3"}, 0)
	if !strings.Contains(got, style.Subtle("gpt-5.3")) {
		t.Errorf("expected the identifier to carry its style, got %q", got)
	}
	if strings.HasPrefix(got, "\x1b") {
		t.Errorf("expected the unstyled column to stay unstyled, got %q", got)
	}

	if got := styled.Header(0); strings.Contains(got, "\x1b") {
		t.Errorf("expected the header to be unstyled, got %q", got)
	}
}

func TestAnElidedStyledCellClosesItsStyle(t *testing.T) {
	styled := New(Column{Title: "Identifier", Width: 20, Style: style.Subtle})

	got := styled.Row([]string{"a-very-long-identifier"}, 8)
	if !strings.HasSuffix(style.Plain(got), width.Ellipsis) {
		t.Errorf("expected the cell to be elided, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("expected the style to be closed, got %q", got)
	}
}
