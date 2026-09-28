package width

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/escape"
)

func TestWrappingBreaksAtSpacesAndMidWordWhereThereAreNone(t *testing.T) {
	for _, test := range []struct {
		text  string
		cells int
		want  []string
	}{
		{"", 8, []string{""}},
		{"hello", 5, []string{"hello"}},
		{"hello!", 5, []string{"hello", "!"}},
		{"one two three", 7, []string{"one two", "three"}},
		{"one two three", 3, []string{"one", "two", "thr", "ee"}},
		{"    unbroken", 8, []string{"    unbr", "oken"}},
		{"  x", 1, []string{" ", "x"}},
		{"日本語です", 4, []string{"日本", "語で", "す"}},
		{"a日b", 2, []string{"a", "日", "b"}},
		{"test 🖊 ", 7, []string{"test 🖊 "}},
		{"👨‍👩‍👧x", 2, []string{"👨‍👩‍👧", "x"}},
		{"one\ntwo", 8, []string{"one", "two"}},
		{"hello", 0, []string{"hello"}},
	} {
		if got := Wrap(test.text, test.cells); !slices.Equal(got, test.want) {
			t.Errorf("Wrap(%q, %d) = %q, want %q", test.text, test.cells, got, test.want)
		}
	}
}

func TestPlainWrappingMatchesPresentationWrapping(t *testing.T) {
	for _, text := range []string{
		"",
		"hello",
		"one two three",
		"    unbroken",
		"  x",
		"words   with spaces",
		"trailing spaces   ",
		"averylongidentifier",
	} {
		for cells := -1; cells <= len(text)+1; cells++ {
			got := wrapPlainLine(text, cells, 7)
			want := wrapLineWithPresentation(text, cells, 7)
			if !slices.Equal(got, want) {
				t.Errorf("wrapPlainLine(%q, %d) = %+v, want %+v", text, cells, got, want)
			}
		}
	}
}

func TestIndentedWrappingAlignsContinuationRows(t *testing.T) {
	got := WrapIndented("one two three four\nfive six seven", 10, 4)
	want := []string{"one two", "    three", "    four", "five six", "    seven"}
	if !slices.Equal(got, want) {
		t.Errorf("WrapIndented() = %q, want %q", got, want)
	}
}

func TestIndentedWrappingUsesTheFirstRowsPathColumns(t *testing.T) {
	got := WrapIndented("tag abcdefghij", 8, 4)
	want := []string{"tag abcd", "    efgh", "    ij"}
	if !slices.Equal(got, want) {
		t.Errorf("WrapIndented() = %q, want %q", got, want)
	}
}

func TestSizedTextWrapsAtItsDeclaredWidth(t *testing.T) {
	fish := "\x1b]66;s=2:n=3:d=4:w=2;🐟\x1b\\"
	got := Wrap(fish+" hi", 6)
	want := []string{fish, "hi"}

	if !slices.Equal(got, want) {
		t.Errorf("Wrap() = %q, want %q", got, want)
	}
}

func FuzzWrappingTerminalTextTerminatesWithValidWidths(fuzzer *testing.F) {
	for _, text := range []string{
		"plain words",
		"日本語",
		"\x1b[31mred words\x1b[0m",
		"\x1b[4Cright",
		"\x1b[999999999999999999999Cright",
		"\x1b]66;s=2:w=2;🐟\x1b\\ after",
		"\x1b]66;s=-1:w=999999999999999999999;x",
		"\x1b]8;;https://example.test\x1b\\linked words\x1b]8;;\x1b\\",
	} {
		fuzzer.Add(text, uint8(20))
	}

	fuzzer.Fuzz(func(t *testing.T, text string, rawColumns uint8) {
		columns := int(rawColumns%80) + 1
		rows := Wrap(text, columns)
		if len(rows) == 0 {
			t.Fatalf("Wrap(%q, %d) returned no rows", text, columns)
		}
		if len(rows) > len([]rune(text))+1 {
			t.Fatalf("Wrap(%q, %d) returned an implausible %d rows", text, columns, len(rows))
		}
		for _, row := range rows {
			if cells := Of(row); cells < 0 {
				t.Fatalf("Wrap(%q, %d) returned a row with %d cells", text, columns, cells)
			}
		}
	})
}

func TestAHyperlinkThatSpansABreakIsClosedAndOpenedAgain(t *testing.T) {
	opening := "\x1b]8;;https://example.test\x1b\\"
	got := Wrap(opening+"\x1b[1mlinked words\x1b[0m"+escape.HyperlinkClose, 6)

	if len(got) != 2 {
		t.Fatalf("expected two rows, got %q", got)
	}

	for i, row := range got {
		if !strings.HasPrefix(row, opening+"\x1b[1m") || !strings.HasSuffix(row, reset+escape.HyperlinkClose) {
			t.Errorf("row %d does not contain a balanced hyperlink and style: %q", i, row)
		}
	}

	if visible := []string{plain(got[0]), plain(got[1])}; !slices.Equal(visible, []string{"linked", "words"}) {
		t.Errorf("visible rows = %q", visible)
	}
}

func TestAStyleThatSpansABreakIsClosedAndOpenedAgain(t *testing.T) {
	got := Wrap("\x1b[1mbold words here\x1b[0m", 10)

	if len(got) != 2 {
		t.Fatalf("expected two rows, got %q", got)
	}

	if !strings.HasPrefix(got[0], "\x1b[1m") || !strings.HasSuffix(got[0], reset) {
		t.Errorf("expected the first row to open and close the style, got %q", got[0])
	}

	if !strings.HasPrefix(got[1], "\x1b[1m") {
		t.Errorf("expected the second row to open the style again, got %q", got[1])
	}
}

func TestNoRowIsWiderThanItWasAskedFor(t *testing.T) {
	text := "\x1b[1mA reasonably long\x1b[0m sentence with 日本語 in it and averylongidentifier too"

	for cells := 2; cells <= 40; cells++ {
		for _, row := range Wrap(text, cells) {
			if got := Of(plain(row)); got > cells {
				t.Errorf("Wrap(_, %d) gave a row of %d cells: %q", cells, got, row)
			}
		}
	}
}

func TestFoldingFillsEveryRowToTheEdgeAndBreaksItSoftly(t *testing.T) {
	soft := func(text string) ScreenRow { return ScreenRow{Text: text, HasSoftBreak: true} }
	hard := func(text string) ScreenRow { return ScreenRow{Text: text} }

	for _, test := range []struct {
		text  string
		cells int
		want  []ScreenRow
	}{
		{"", 8, []ScreenRow{hard("")}},
		{"hello", 5, []ScreenRow{hard("hello")}},
		{"hello!", 5, []ScreenRow{soft("hello"), hard("!")}},
		{"one two three", 5, []ScreenRow{soft("one t"), soft("wo th"), hard("ree")}},
		{"ab   cd", 3, []ScreenRow{soft("ab "), soft("  c"), hard("d")}},
		{"a日b", 2, []ScreenRow{soft("a"), soft("日"), hard("b")}},
		{"one\ntwo", 8, []ScreenRow{hard("one"), hard("two")}},
		{"hello", 0, []ScreenRow{hard("hello")}},
	} {
		if got := Fold(test.text, test.cells); !slices.Equal(got, test.want) {
			t.Errorf("Fold(%q, %d) = %+v, want %+v", test.text, test.cells, got, test.want)
		}
	}
}

func TestFoldingKeepsEveryCharacterSoTheRowsJoinBackIntoTheLine(t *testing.T) {
	line := "cd /home/somebody/project && OH_PROFILE=/tmp/profiles oh -c r \"think it through\""

	for cells := 1; cells <= len(line)+1; cells++ {
		rows := Fold(line, cells)

		if joined := strings.Join(Texts(rows), ""); plain(joined) != line {
			t.Errorf("Fold(_, %d) joined back into %q", cells, plain(joined))
		}
		for i, row := range rows {
			if isLast := i == len(rows)-1; row.HasSoftBreak == isLast {
				t.Errorf("Fold(_, %d) row %d of %d broke wrongly: %+v", cells, i, len(rows), row)
			}
			if got := Of(row.Text); got > cells {
				t.Errorf("Fold(_, %d) gave a row of %d cells: %q", cells, got, row.Text)
			}
		}
	}
}

func TestFoldingClosesAStyleAtTheBreakAndOpensItAgainAfter(t *testing.T) {
	got := Texts(Fold("\x1b[31mredder\x1b[0m", 3))
	want := []string{"\x1b[31mred" + reset, "\x1b[31mder\x1b[0m"}

	if !slices.Equal(got, want) {
		t.Errorf("Fold() = %q, want %q", got, want)
	}
}

func TestRowsKeepTheirTextInEitherDirection(t *testing.T) {
	texts := []string{"one", "", "three"}

	if got := Texts(HardRows(texts)); !slices.Equal(got, texts) {
		t.Errorf("Texts(HardRows(%q)) = %q", texts, got)
	}
	for _, row := range HardRows(texts) {
		if row.HasSoftBreak {
			t.Errorf("HardRows broke %q softly", row.Text)
		}
	}
}

func plain(text string) string {
	var out strings.Builder

	for _, one := range split(text) {
		if !one.isEscape {
			out.WriteString(one.text)
		}
	}

	return out.String()
}

func TestElidingPlainText(t *testing.T) {
	const text = "chewy-sardine"

	tests := map[int]string{
		0:  "",
		1:  "…",
		5:  "chew…",
		12: "chewy-sardi…",
		13: "chewy-sardine",
		20: "chewy-sardine",
	}

	for cells, wanted := range tests {
		if got := Elide(text, cells); got != wanted {
			t.Errorf("Elide(%q, %d) = %q, want %q", text, cells, got, wanted)
		}
	}
}

func TestElidingPlainTextFromTheStartKeepsItsTail(t *testing.T) {
	const text = "chewy/sardine"

	tests := map[int]string{
		0:  "",
		1:  "…",
		5:  "…dine",
		12: "…ewy/sardine",
		13: "chewy/sardine",
		20: "chewy/sardine",
	}

	for cells, wanted := range tests {
		if got := ElideStart(text, cells); got != wanted {
			t.Errorf("ElideStart(%q, %d) = %q, want %q", text, cells, got, wanted)
		}
	}

	if got := ElideStart("ab界", 3); got != "…界" {
		t.Errorf("ElideStart kept %q of a wide tail", got)
	}
}

func TestElidingStyledTextClosesTheStyleItCutsThrough(t *testing.T) {
	const text = "\x1b[31mchewy-sardine\x1b[0m"

	if got := Elide(text, 13); got != text {
		t.Errorf("expected styled text that fits to be left alone, got %q", got)
	}

	got := Elide(text, 5)
	if want := "\x1b[31mchew…\x1b[0m"; got != want {
		t.Errorf("Elide(styled, 5) = %q, want %q", got, want)
	}
	if Of(got) != 5 {
		t.Errorf("expected 5 cells, got %d in %q", Of(got), got)
	}
}

func TestElidingLeavesAlreadyClosedStylingAlone(t *testing.T) {
	const text = "\x1b[31mred\x1b[0m and more text"

	got := Elide(text, 8)
	if want := "\x1b[31mred\x1b[0m and…"; got != want {
		t.Errorf("Elide(%q, 8) = %q, want %q", text, got, want)
	}
	if strings.Count(got, reset) != 1 {
		t.Errorf("expected the one reset it already had, got %q", got)
	}
}

func TestElidingNeverCutsThroughAnEscapeSequence(t *testing.T) {
	const text = "abc\x1b[38;2;150;152;150mdefghij\x1b[0m"

	for cells := range 12 {
		got := Elide(text, cells)

		if Of(got) > cells {
			t.Errorf("Elide(%d) = %q, which is %d cells", cells, got, Of(got))
		}

		runes := []rune(got)
		for i := 0; i < len(runes); i++ {
			if runes[i] != '\x1b' {
				continue
			}

			sequence := escape.GetSequence(runes, i)
			if sequence.End > len(runes) || sequence.End == i {
				t.Fatalf("Elide(%d) = %q, which holds a cut sequence at %d", cells, got, i)
			}
			if !strings.HasSuffix(string(runes[i:sequence.End]), "m") {
				t.Errorf("Elide(%d) = %q, whose sequence at %d is incomplete", cells, got, i)
			}
			i = sequence.End - 1
		}
	}
}
