package style

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
)

func TestEveryPaintIsNormalisedHoweverItIsWritten(t *testing.T) {
	for written, want := range map[string]Paint{
		"":                            "",
		"   ":                         "",
		"default":                     "",
		"DEFAULT":                     "",
		" #ABCDEF ":                   "#abcdef",
		"default italic":              "italic",
		"italic default":              "italic",
		"  bold   #010203  ":          "bold #010203",
		"UNDERLINE:CURLY":             "underline:curly",
		"underline:#ABCDEF":           "underline:#abcdef",
		"\tbold\nitalic\tunderline\n": "bold italic underline",
	} {
		var value Paint
		if err := value.UnmarshalText([]byte(written)); err != nil {
			t.Fatalf("%q: %v", written, err)
		}
		if value != want {
			t.Errorf("%q became %q, want %q", written, value, want)
		}
	}
}

func TestANormalisedPaintIsAlreadyNormalised(t *testing.T) {
	for _, written := range []string{
		"default",
		"#010203 bold italic underline:curly underline:#040506",
		"faint overline",
	} {
		var once Paint
		if err := once.UnmarshalText([]byte(written)); err != nil {
			t.Fatalf("%q: %v", written, err)
		}

		var twice Paint
		if err := twice.UnmarshalText([]byte(once)); err != nil {
			t.Fatalf("%q: %v", once, err)
		}

		if once != twice {
			t.Errorf("%q settled at %q and then %q", written, once, twice)
		}
	}
}

func TestAPaintRefusesWhatNoTerminalCouldDraw(t *testing.T) {
	for written, want := range map[string]string{
		"blue":                 "not a colour or a decoration",
		"#12345":               "not a colour or a decoration",
		"#1234567":             "not a colour or a decoration",
		"#gggggg":              "not a colour or a decoration",
		"010203":               "not a colour or a decoration",
		"bolder":               "not a colour or a decoration",
		"bold italics":         "not a colour or a decoration",
		"\x1b[31m":             "not a colour or a decoration",
		"31;1":                 "not a colour or a decoration",
		"underline:":           "not an underline",
		"underline:wobbly":     "not an underline",
		"underline:#12345":     "not an underline",
		"#010203 #040506":      "a second colour",
		"#010203 bold #010203": "a second colour",
	} {
		var value Paint
		err := value.UnmarshalText([]byte(written))
		if err == nil {
			t.Fatalf("%q was accepted as %q", written, value)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q was refused with %q, want it to mention %q", written, err, want)
		}
		if value != "" {
			t.Errorf("%q left %q behind on a key it could not paint", written, value)
		}
	}
}

func TestARefusalLeavesTheKeyItCameFromAlone(t *testing.T) {
	value := Paint("#010203 bold")

	if err := value.UnmarshalText([]byte("chartreuse")); err == nil {
		t.Fatal("expected the refusal")
	}
	if value != "#010203 bold" {
		t.Errorf("the key became %q, want what it already was", value)
	}
}

func TestASequenceCarriesNothingButItsOwnParameters(t *testing.T) {
	for _, written := range []string{
		"",
		"default",
		"#010203",
		"bold faint italic underline blink reverse hidden strikethrough overline",
		"underline:dashed underline:#040506 #010203",
	} {
		var value Paint
		if err := value.UnmarshalText([]byte(written)); err != nil {
			t.Fatalf("%q: %v", written, err)
		}

		for _, layer := range []string{foregroundLayer, backgroundLayer} {
			sequence := paintSequence(value, layer)
			if strings.Trim(sequence, "0123456789;:") != "" {
				t.Errorf("%q drew %q, which is not a parameter list", written, sequence)
			}
			if (sequence == "") != (value == "") {
				t.Errorf("%q drew %q", written, sequence)
			}
		}
	}
}

func TestEveryDecorationIsDrawnOnTopOfEveryOther(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte(
		"strikethrough overline hidden reverse blink underline faint italic bold #010203",
	)); err != nil {
		t.Fatal(err)
	}

	if got, want := foregroundSequence(value), "1;2;3;4;5;7;8;9;53;38;2;1;2;3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEveryThemeKeyIsCompiledWithItsDecorations(t *testing.T) {
	theme := Theme{}
	keys := reflect.ValueOf(&theme).Elem()

	for _, key := range keys.Fields() {
		key.SetString("bold #010203")
	}

	written := 0

	for field, code := range reflect.ValueOf(compileTheme(theme)).Elem().Fields() {
		if code.Kind() != reflect.String {
			continue
		}

		written++

		if got := code.String(); got != "1;38;2;1;2;3" && got != "1;48;2;1;2;3" {
			t.Errorf("%s compiled to %q, want a bold #010203 of one layer or the other", field.Name, got)
		}
	}

	if written != keys.NumField() {
		t.Errorf("%d of the %d theme keys were compiled", written, keys.NumField())
	}
}

func TestEveryThemeKeyCanBeWrittenInAConfig(t *testing.T) {
	theme := DefaultTheme()

	for field, key := range reflect.ValueOf(&theme).Elem().Fields() {
		written := key.String()

		var value Paint
		if err := value.UnmarshalText([]byte(written)); err != nil {
			t.Errorf("the default %s is refused: %v", field.Tag.Get("toml"), err)
		}
		if value != Paint(written) {
			t.Errorf("the default %q is normalised to %q", written, value)
		}
	}
}

func TestAKeySetInCodeIsReadWithoutBeingNormalisedFirst(t *testing.T) {
	if got, want := foregroundSequence("default italic"), italicCode; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAKeyNobodyCouldReadPaintsNothingRatherThanRubbish(t *testing.T) {
	if got := foregroundSequence("chartreuse"); got != "" {
		t.Errorf("got %q, want nothing painted", got)
	}
}

func TestAKeyWithoutAColourStillReportsTheGraphicColourItFallsBackTo(t *testing.T) {
	theme := DefaultTheme()
	theme.Dim = "italic"
	t.Cleanup(ApplyTheme(theme))

	fallback, _ := colour(string(DefaultTheme().Dim))
	if got := DimColour(); got != fallback {
		t.Errorf("got %v, want the default dim %v", got, fallback)
	}
}

func TestAKeyNamingAColourReportsThatColourWhateverItsDecorations(t *testing.T) {
	theme := DefaultTheme()
	theme.StatusDanger = "underline:curly #010203 blink"
	t.Cleanup(ApplyTheme(theme))

	if got, want := FailureColour(), (color.RGBA{R: 1, G: 2, B: 3, A: 0xff}); got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAThemeColourAcceptsTheTerminalDefault(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte(" DEFAULT ")); err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Errorf("got %q, want the terminal default", value)
	}
}

func TestAThemeColourRefusesAnythingOtherThanSixDigitHex(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte("blue")); err == nil {
		t.Fatal("expected a named colour to be refused")
	}
}

func TestAThemeKeyCarriesItsDecorationsBesideItsColour(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte(" #010203  ITALIC underline ")); err != nil {
		t.Fatal(err)
	}
	if value != "#010203 italic underline" {
		t.Fatalf("got %q", value)
	}

	enableColor(t)

	theme := DefaultTheme()
	theme.Accent = value
	t.Cleanup(ApplyTheme(theme))

	if got, want := Subject("subject"), "\x1b[3;4;38;2;1;2;3msubject"+reset; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAThemeKeyCanBeDecoratedWithoutNamingAColour(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte("default underline")); err != nil {
		t.Fatal(err)
	}
	if value != "underline" {
		t.Fatalf("got %q", value)
	}

	enableColor(t)

	theme := DefaultTheme()
	theme.Normal = value
	t.Cleanup(ApplyTheme(theme))

	if got, want := Answer("answer"), "\x1b[4manswer"+reset; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestABackgroundKeyCarriesItsDecorationsToo(t *testing.T) {
	enableColor(t)

	theme := DefaultTheme()
	theme.User = "#040506 italic"
	t.Cleanup(ApplyTheme(theme))

	if got, want := User("user"), "\x1b[3;48;2;4;5;6muser"+reset; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEveryDecorationHasItsOwnCode(t *testing.T) {
	for word, want := range map[string]string{
		"bold":          "1",
		"faint":         "2",
		"italic":        "3",
		"underline":     "4",
		"blink":         "5",
		"reverse":       "7",
		"hidden":        "8",
		"strikethrough": "9",
		"overline":      "53",
	} {
		var value Paint
		if err := value.UnmarshalText([]byte(word)); err != nil {
			t.Fatalf("%s: %v", word, err)
		}
		if got := foregroundSequence(value); got != want {
			t.Errorf("%s drew %q, want %q", word, got, want)
		}
	}
}

func TestAnUnderlineTakesAShapeAndAColourOfItsOwn(t *testing.T) {
	for written, want := range map[string]string{
		"underline:single":                  "4",
		"underline:double":                  "4:2",
		"underline:curly":                   "4:3",
		"underline:dotted":                  "4:4",
		"underline:dashed":                  "4:5",
		"underline:#010203":                 "4;58;2;1;2;3",
		"underline:curly underline:#010203": "4:3;58;2;1;2;3",
	} {
		var value Paint
		if err := value.UnmarshalText([]byte(written)); err != nil {
			t.Fatalf("%s: %v", written, err)
		}
		if got := foregroundSequence(value); got != want {
			t.Errorf("%s drew %q, want %q", written, got, want)
		}
	}
}

func TestDecorationsAreDrawnInTheOrderTheTerminalReadsThem(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte("overline #010203 underline:curly bold")); err != nil {
		t.Fatal(err)
	}

	if got, want := foregroundSequence(value), "1;4:3;53;38;2;1;2;3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := backgroundSequence(value), "1;4:3;53;48;2;1;2;3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnUnderlineRefusesAShapeNobodyDraws(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte("underline:wobbly")); err == nil {
		t.Fatal("expected an unknown underline shape to be refused")
	}
}

func TestAThemeKeyRefusesAnUnknownDecoration(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte("#010203 blinking")); err == nil {
		t.Fatal("expected an unknown decoration to be refused")
	}
}

func TestAThemeKeyRefusesASecondColour(t *testing.T) {
	var value Paint
	if err := value.UnmarshalText([]byte("#010203 #040506")); err == nil {
		t.Fatal("expected a second colour to be refused")
	}
}
