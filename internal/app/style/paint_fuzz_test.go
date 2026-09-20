package style

import (
	"strings"
	"testing"
)

const fuzzedPaintLength = 128

func FuzzAPaintEmitsNothingButTheParametersItWasWritten(fuzzer *testing.F) {
	for _, seed := range []string{
		"",
		"default",
		"#010203",
		"#010203 bold",
		"faint italic underline blink reverse hidden strikethrough overline",
		"underline:curly",
		"underline:#040506",
		"underline:single underline:double",
		"#010203 #040506",
		"blue",
		"\x1b[31m",
		"underline:",
		"#01020",
		"bold\tbold  bold",
		"58;2;1;2;3",
		"underline:#01020g",
	} {
		fuzzer.Add(seed)
	}

	fuzzer.Fuzz(func(t *testing.T, written string) {
		if len(written) > fuzzedPaintLength {
			t.Skip("longer than a theme key is ever written")
		}

		value := Paint("#010203 bold")

		if err := value.UnmarshalText([]byte(written)); err != nil {
			if value != "#010203 bold" {
				t.Fatalf("a refusal left %q behind", value)
			}
			return
		}

		requireSettledPaint(t, value)
		requireBareParameters(t, value)
		requireTheTextIsLeftAlone(t, value)
	})
}

func requireSettledPaint(t *testing.T, value Paint) {
	t.Helper()

	var again Paint
	if err := again.UnmarshalText([]byte(value)); err != nil {
		t.Fatalf("%q was accepted and then refused: %v", value, err)
	}
	if again != value {
		t.Fatalf("%q settled at %q", value, again)
	}
	if strings.ToLower(string(value)) != string(value) {
		t.Fatalf("%q was kept as it was written", value)
	}
	if strings.Contains(string(value), defaultColour) {
		t.Fatalf("%q kept a word that means nothing on its own", value)
	}
}

func requireBareParameters(t *testing.T, value Paint) {
	t.Helper()

	for _, layer := range []string{foregroundLayer, backgroundLayer} {
		sequence := paintSequence(value, layer)

		if strings.Trim(sequence, "0123456789;:") != "" {
			t.Fatalf("%q drew %q, which is not a parameter list", value, sequence)
		}
		if (sequence == "") != (value == "") {
			t.Fatalf("%q drew %q", value, sequence)
		}
		if strings.HasPrefix(sequence, ";") || strings.HasSuffix(sequence, ";") {
			t.Fatalf("%q drew %q, which has an empty parameter", value, sequence)
		}
	}
}

func requireTheTextIsLeftAlone(t *testing.T, value Paint) {
	t.Helper()

	enableColor(t)

	theme := DefaultTheme()
	theme.Normal = value
	theme.User = value
	defer ApplyTheme(theme)()

	const text = "the quick brown fox"

	for name, paint := range map[string]Style{"normal": Normal, "user": User} {
		if got := Plain(paint(text)); got != text {
			t.Fatalf("%q painted the %s text as %q", value, name, got)
		}
	}
}
