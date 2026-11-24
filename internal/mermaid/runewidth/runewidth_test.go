package runewidth

import "testing"

func TestWidthsUseTerminalCells(t *testing.T) {
	if got := StringWidth("a界"); got != 3 {
		t.Errorf("got string width %d, want 3", got)
	}
	if got := Cells("👩‍🚀"); len(got) != 2 || got[0] != "👩‍🚀" || got[1] != "" {
		t.Errorf("got cells %#v, want one two-cell grapheme", got)
	}
}
