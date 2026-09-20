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
