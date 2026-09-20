package escape

import "testing"

func TestGetEndIncludesTheEscapeTerminator(t *testing.T) {
	for _, sequence := range []string{"\x1b[38;2;255;255;255m", "\x1b[K"} {
		runes := []rune(sequence + "after")
		if got := GetEnd(runes, 0); got != len([]rune(sequence)) {
			t.Errorf("GetEnd(%q) = %d, want %d", sequence, got, len([]rune(sequence)))
		}
	}
}

func TestGetEndTakesAnUnterminatedEscapeToTheEnd(t *testing.T) {
	runes := []rune("\x1b[38;2")
	if got := GetEnd(runes, 0); got != len(runes) {
		t.Errorf("GetEnd() = %d, want %d", got, len(runes))
	}
}

func TestGetSequenceDescribesSizedText(t *testing.T) {
	sequence := "\x1b]66;s=2:n=3:d=4:w=2;🐟\x1b\\"
	runes := []rune(sequence + "after")
	got := GetSequence(runes, 0)

	if got.End != len([]rune(sequence)) {
		t.Errorf("end = %d, want %d", got.End, len([]rune(sequence)))
	}
	if got.Text != "🐟" {
		t.Errorf("text = %q, want fish", got.Text)
	}
	if got.Cells != 4 {
		t.Errorf("cells = %d, want 4", got.Cells)
	}
}

func TestGetSequenceDescribesHyperlinkChanges(t *testing.T) {
	opening := "\x1b]8;;https://example.test\x1b\\"
	opened := GetSequence([]rune(opening), 0)
	if !opened.IsHyperlink || opened.Hyperlink != opening {
		t.Errorf("opening sequence = %+v", opened)
	}

	closed := GetSequence([]rune(HyperlinkClose), 0)
	if !closed.IsHyperlink || closed.Hyperlink != "" {
		t.Errorf("closing sequence = %+v", closed)
	}
}

func TestGetSequenceDescribesRightwardCursorMovement(t *testing.T) {
	got := GetSequence([]rune("\x1b[4Cafter"), 0)
	if got.End != len([]rune("\x1b[4C")) || got.Cells != 4 || got.IsStyle {
		t.Errorf("GetSequence() = %+v, want a four-cell cursor move", got)
	}
}

func TestMalformedSizedTextOccupiesNoCells(t *testing.T) {
	for _, sequence := range []string{
		"\x1b]66;s=2:w=2;🐟",
		"\x1b]66;s=0:w=2;🐟\x1b\\",
		"\x1b]66;s=8:w=2;🐟\x1b\\",
		"\x1b]66;s=no:w=2;🐟\x1b\\",
		"\x1b]66;s=2;🐟\x1b\\",
		"\x1b]66;s=2:w=0;🐟\x1b\\",
		"\x1b]66;s=2:w=8;🐟\x1b\\",
		"\x1b]66;s=2:w=no;🐟\x1b\\",
		"\x1b]66;s=2:w=2;\x1b\\",
	} {
		got := GetSequence([]rune(sequence), 0)
		if got.Cells != 0 || got.Text != "" {
			t.Errorf("GetSequence(%q) = %+v, want no visible cells", sequence, got)
		}
	}
}

func FuzzGetSequence(fuzzer *testing.F) {
	for _, text := range []string{
		"plain",
		"\x1b",
		"\x1b[31mred\x1b[0m",
		"\x1b[4Cright",
		"\x1b[999999999999999999999Cright",
		"\x1b]66;s=2:w=2;🐟\x1b\\",
		"\x1b]66;s=-999999999999999999:w=2;x",
		"\x1b]8;;https://example.test\x1b\\link\x1b]8;;\x1b\\",
	} {
		fuzzer.Add(text)
	}

	fuzzer.Fuzz(func(t *testing.T, text string) {
		runes := []rune(text)
		for start, character := range runes {
			if character != '\x1b' {
				continue
			}

			sequence := GetSequence(runes, start)
			if sequence.End <= start || sequence.End > len(runes) {
				t.Fatalf("GetSequence(%q, %d) ended at %d", text, start, sequence.End)
			}
			if sequence.Cells < 0 || sequence.Cells > maximumCursorCells {
				t.Fatalf("GetSequence(%q, %d) occupies %d cells", text, start, sequence.Cells)
			}
			if sequence.Text != "" && sequence.Cells > 49 {
				t.Fatalf("GetSequence(%q, %d) has oversized text: %+v", text, start, sequence)
			}
			if sequence.Cells == 0 && sequence.Text != "" {
				t.Fatalf("GetSequence(%q, %d) has text without cells: %+v", text, start, sequence)
			}
			if sequence.IsStyle && sequence.Cells != 0 {
				t.Fatalf("GetSequence(%q, %d) is both style and text: %+v", text, start, sequence)
			}
			if sequence.IsHyperlink && (sequence.IsStyle || sequence.Cells != 0 || sequence.Text != "") {
				t.Fatalf("GetSequence(%q, %d) describes a malformed hyperlink: %+v", text, start, sequence)
			}
		}
	})
}

func TestAStringSequenceEndsAtItsTerminator(t *testing.T) {
	for _, test := range []struct {
		name     string
		sequence string
	}{
		{name: "a graphics command", sequence: "\x1b_Ga=T,f=32,m=1;AAmAKA\x1b\\"},
		{name: "a device control string", sequence: "\x1bP+q544e\x1b\\"},
		{name: "a privacy message", sequence: "\x1b^something\x1b\\"},
		{name: "an application program command ended with a bell", sequence: "\x1b_Ga=q;OK\a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runes := []rune(test.sequence + "after")

			got := GetSequence(runes, 0)
			if got.End != len([]rune(test.sequence)) {
				t.Errorf("ended at %d, want %d", got.End, len([]rune(test.sequence)))
			}
			if got.Cells != 0 {
				t.Errorf("occupies %d cells, want none", got.Cells)
			}
		})
	}
}

func TestAnUnterminatedStringSequenceTakesTheRest(t *testing.T) {
	runes := []rune("\x1b_Ga=T,f=32;AAmAKA")
	if got := GetEnd(runes, 0); got != len(runes) {
		t.Errorf("GetEnd() = %d, want %d", got, len(runes))
	}
}
