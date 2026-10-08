package style

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
)

var everyBackground = []Background{DarkBackground, GreyDarkBackground, GreyLightBackground, LightBackground}

func TestTheBackgroundOfACommonTerminalThemeIsReadAsItLooks(t *testing.T) {
	for value, want := range map[string]Background{
		"#000000": DarkBackground,
		"#242424": DarkBackground,
		"#282c34": DarkBackground,
		"#002b36": DarkBackground,
		"#2e3440": DarkBackground,
		"#3a3a3a": DarkBackground,
		"#4c566a": GreyDarkBackground,
		"#505050": GreyDarkBackground,
		"#586e75": GreyDarkBackground,
		"#707070": GreyDarkBackground,
		"#7c6f64": GreyDarkBackground,
		"#808080": GreyLightBackground,
		"#93a1a1": GreyLightBackground,
		"#a0a0a0": GreyLightBackground,
		"#c0c0c0": GreyLightBackground,
		"#d0d0d0": LightBackground,
		"#e1e2e7": LightBackground,
		"#eff1f5": LightBackground,
		"#f6f8fa": LightBackground,
		"#fdf6e3": LightBackground,
		"#ffffff": LightBackground,
	} {
		if got := BackgroundOf(mustColour(value)); got != want {
			t.Errorf("%s was read as %s, want %s", value, got, want)
		}
	}
}

func TestEveryBackgroundIsNamedAfterTheAppearanceThatChoosesIt(t *testing.T) {
	for _, background := range everyBackground {
		var appearance Appearance
		if err := appearance.UnmarshalText([]byte(background.String())); err != nil {
			t.Fatal(err)
		}
		if got := appearance.On(DarkBackground); got != background {
			t.Errorf("%q chose %s", appearance, got)
		}
	}
}

func TestAnAppearanceIsReadInAnyCaseAndAnythingElseIsRefused(t *testing.T) {
	for written, want := range map[string]Appearance{
		"auto":       AutomaticAppearance,
		"Dark":       DarkAppearance,
		"GREY-DARK":  GreyDarkAppearance,
		"grey-light": GreyLightAppearance,
		"LIGHT":      LightAppearance,
	} {
		var got Appearance
		if err := got.UnmarshalText([]byte(written)); err != nil || got != want {
			t.Errorf("%q was read as %q (%v), want %q", written, got, err, want)
		}
	}

	for _, refused := range []string{"dim", "gray-dark", "grey", ""} {
		var value Appearance
		err := value.UnmarshalText([]byte(refused))
		if err == nil || !strings.Contains(err.Error(), `"auto", "dark", "grey-dark", "grey-light", or "light"`) {
			t.Errorf("%q gave %v, want a refusal naming the appearances", refused, err)
		}
	}
}

func TestAnAutomaticAppearanceFollowsTheTerminalAndAChosenOneDoesNot(t *testing.T) {
	for _, reported := range everyBackground {
		for _, appearance := range []Appearance{"", AutomaticAppearance} {
			if got := appearance.On(reported); got != reported {
				t.Errorf("%q on a %s background chose %s", appearance, reported, got)
			}
		}
		for _, chosen := range everyBackground {
			if got := Appearance(chosen.String()).On(reported); got != chosen {
				t.Errorf("%s on a %s background chose %s", chosen, reported, got)
			}
		}
	}
}

func TestEveryBackgroundIsDrawnInItsOwnPalette(t *testing.T) {
	enableColor(t)
	t.Cleanup(ApplyTheme(DefaultTheme()))

	for _, background := range everyBackground {
		restore := ReportBackground(background)
		palette := DefaultTheme().PaletteOn(background)

		if got, want := Accent("x"), "\x1b["+foregroundSequence(palette.Accent)+"m"; !strings.HasPrefix(got, want) {
			t.Errorf("a %s background drew the accent %q, want %q", background, got, want)
		}
		if got, want := User("x"), "\x1b["+backgroundSequence(palette.User)+"m"; !strings.HasPrefix(got, want) {
			t.Errorf("a %s background drew the user panel %q, want %q", background, got, want)
		}
		gradient := simulationGradients[background]
		if got := Simulation("Simulation"); !strings.Contains(got, sequenceFor(gradient.from)) ||
			!strings.Contains(got, sequenceFor(gradient.to)) {
			t.Errorf("a %s background drew the gradient %q", background, got)
		}

		restore()
	}

	if got, want := Accent("x"), "\x1b["+foregroundSequence(DefaultTheme().Dark.Accent)+"m"; !strings.HasPrefix(got, want) {
		t.Errorf("restoring the background drew %q, want the dark accent", got)
	}
}

func TestEveryPaletteIsItsOwn(t *testing.T) {
	theme := DefaultTheme()
	seen := map[Paint]Background{}

	for _, background := range everyBackground {
		accent := theme.PaletteOn(background).Accent
		if earlier, isSeen := seen[accent]; isSeen {
			t.Errorf("the %s and %s palettes share the accent %s", earlier, background, accent)
		}
		seen[accent] = background
	}
}

func TestAThemeAppliedOnAReportedBackgroundStaysOnIt(t *testing.T) {
	enableColor(t)
	t.Cleanup(ReportBackground(GreyLightBackground))

	theme := DefaultTheme()
	theme.GreyLight.User = "#010203"
	t.Cleanup(ApplyTheme(theme))

	if got := User("x"); !strings.Contains(got, "48;2;1;2;3") {
		t.Errorf("a theme applied after the background was reported drew %q, want its grey-light panel", got)
	}
}

func TestAChosenAppearanceOutranksTheReportedBackground(t *testing.T) {
	enableColor(t)
	t.Cleanup(ReportBackground(LightBackground))

	theme := DefaultTheme()
	theme.Appearance = GreyDarkAppearance
	t.Cleanup(ApplyTheme(theme))

	want := "\x1b[" + backgroundSequence(DefaultTheme().GreyDark.Harness) + "m"
	if got := Harness("x"); !strings.HasPrefix(got, want) {
		t.Errorf("a grey-dark appearance on a light background drew %q, want the grey-dark panel", got)
	}
}

func TestAGraphicColourFallsBackToTheDefaultOfItsOwnPalette(t *testing.T) {
	for _, background := range everyBackground {
		restoreBackground := ReportBackground(background)

		theme := DefaultTheme()
		theme.Dark.Dim = "italic"
		theme.GreyDark.Dim = "italic"
		theme.GreyLight.Dim = "italic"
		theme.Light.Dim = "italic"
		restoreTheme := ApplyTheme(theme)

		if got, want := DimColour(), mustColour(string(DefaultTheme().PaletteOn(background).Dim)); got != want {
			t.Errorf("a %s background got %v, want its default dim %v", background, got, want)
		}

		restoreTheme()
		restoreBackground()
	}
}

func TestEveryPanelSitsOnTheSideOfItsBackgroundThatItsTextNeeds(t *testing.T) {
	isDark := map[Background]bool{
		DarkBackground:      true,
		GreyDarkBackground:  true,
		GreyLightBackground: false,
		LightBackground:     false,
	}

	for _, background := range everyBackground {
		palette := DefaultTheme().PaletteOn(background)
		for name, panel := range map[string]Paint{"user": palette.User, "harness": palette.Harness} {
			reading := BackgroundOf(mustColour(string(panel)))
			if isDark[reading] != isDark[background] {
				t.Errorf("the %s %s panel %s reads as %s", background, name, panel, reading)
			}
		}
	}
}

func TestEveryForegroundCanBeReadOnTheBackgroundItIsFor(t *testing.T) {
	for background, test := range map[Background]struct {
		backdrop        string
		minimumContrast float64
	}{
		DarkBackground:      {backdrop: "#242424", minimumContrast: 4},
		GreyDarkBackground:  {backdrop: "#505050", minimumContrast: 4.5},
		GreyLightBackground: {backdrop: "#a0a0a0", minimumContrast: 3.5},
		LightBackground:     {backdrop: "#ffffff", minimumContrast: 4.5},
	} {
		backdrop := mustColour(test.backdrop)
		for name, foreground := range foregroundsOf(background) {
			if got := contrastOf(foreground, backdrop); got < test.minimumContrast {
				t.Errorf(
					"the %s %s has a contrast of %.2f on %s, want at least %.1f",
					background, name, got, test.backdrop, test.minimumContrast,
				)
			}
		}
	}
}

func foregroundsOf(background Background) map[string]color.RGBA {
	foregrounds := map[string]color.RGBA{
		"simulation from": simulationGradients[background].from,
		"simulation to":   simulationGradients[background].to,
	}
	for field, key := range reflect.ValueOf(DefaultTheme().PaletteOn(background)).Fields() {
		if key.String() == "" || field.Name == "User" || field.Name == "Harness" {
			continue
		}
		foregrounds[field.Tag.Get("toml")] = mustColour(key.String())
	}

	return foregrounds
}

func contrastOf(first color.RGBA, second color.RGBA) float64 {
	lighter, darker := luminanceOf(first), luminanceOf(second)
	if darker > lighter {
		lighter, darker = darker, lighter
	}

	return (lighter + 0.05) / (darker + 0.05)
}
