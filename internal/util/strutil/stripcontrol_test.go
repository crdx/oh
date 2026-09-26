package strutil_test

import (
	"testing"

	"crdx.org/oh/internal/util/strutil"
)

func TestStripControlKeepsTextAndNothingElse(t *testing.T) {
	for text, want := range map[string]string{
		"plain text":               "plain text",
		"a\nb\tc":                  "a\nb\tc",
		"a\rb":                     "ab",
		"a\x07b\x08c":              "abc",
		"a\u009bb":                 "ab",
		"x\x1b]52;c;cHduZWQ=\x07y": "xy",
		"x\x1b[2Jy":                "xy",
		"x\x1b[31mredy":            "xredy",
		"\x1b]8;;https://a\x1b\\link\x1b]8;;\x1b\\": "link",
		"\x1b]66;s=2:w=3;big\x1b\\":                 "",
		"tail\x1b":                                  "tail",
		"tail\x1b[":                                 "tail",
		"":                                          "",
	} {
		if got := strutil.StripControl(text); got != want {
			t.Errorf("StripControl(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestControlFreeLengthStopsWhereStrippingWouldChangeSomething(t *testing.T) {
	for _, testCase := range []struct {
		text string
		from int
		want int
	}{
		{text: "", want: 0},
		{text: "plain\ttext\n", want: 11},
		{text: "héllo 🐞", want: 11},
		{text: "before\x1b[31mred", want: 6},
		{text: "before\x1b]8;;https://a", want: 6},
		{text: "a\rb", want: 1},
		{text: "a\u009bb", want: 1},
		{text: "cut \xf0\x9f", want: 4},
		{text: "cut \xf0\x9f\x90\x9e", from: 4, want: 8},
		{text: "resumed after", from: 7, want: 13},
	} {
		if got := strutil.ControlFreeLength(testCase.text, testCase.from); got != testCase.want {
			t.Errorf("ControlFreeLength(%q, %d) = %d, want %d", testCase.text, testCase.from, got, testCase.want)
		}
	}
}

func FuzzControlFreeLengthIsLeftAloneByStripControl(fuzzer *testing.F) {
	for _, text := range []string{"plain", "a\x1b]8;;x\nb", "héllo\u009b", "cut \xf0\x9f", "\x1b[31m"} {
		fuzzer.Add(text)
	}

	fuzzer.Fuzz(func(t *testing.T, text string) {
		clean := text[:strutil.ControlFreeLength(text, 0)]
		if got := strutil.StripControl(clean); got != clean {
			t.Fatalf("StripControl(%q) = %q, but it was counted clean", clean, got)
		}
	})
}
