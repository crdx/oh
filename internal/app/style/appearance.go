package style

import (
	"fmt"
	"image/color"
	"math"
	"strings"
)

type Background int

const (
	DarkBackground Background = iota
	GreyDarkBackground
	GreyLightBackground
	LightBackground
)

func (self Background) String() string {
	switch self {
	case GreyDarkBackground:
		return string(GreyDarkAppearance)
	case GreyLightBackground:
		return string(GreyLightAppearance)
	case LightBackground:
		return string(LightAppearance)
	case DarkBackground:
	}

	return string(DarkAppearance)
}

const (
	greyDarkLuminance  = 0.045
	greyLightLuminance = 0.2
	lightLuminance     = 0.6
)

func BackgroundOf(value color.RGBA) Background {
	switch luminance := luminanceOf(value); {
	case luminance >= lightLuminance:
		return LightBackground
	case luminance >= greyLightLuminance:
		return GreyLightBackground
	case luminance >= greyDarkLuminance:
		return GreyDarkBackground
	default:
		return DarkBackground
	}
}

func luminanceOf(value color.RGBA) float64 {
	return 0.2126*linearChannel(value.R) + 0.7152*linearChannel(value.G) + 0.0722*linearChannel(value.B)
}

func linearChannel(channel uint8) float64 {
	value := float64(channel) / 0xff
	if value <= 0.04045 {
		return value / 12.92
	}

	return math.Pow((value+0.055)/1.055, 2.4)
}

type Appearance string

const (
	AutomaticAppearance Appearance = "auto"
	DarkAppearance      Appearance = "dark"
	GreyDarkAppearance  Appearance = "grey-dark"
	GreyLightAppearance Appearance = "grey-light"
	LightAppearance     Appearance = "light"
)

func (self *Appearance) UnmarshalText(text []byte) error {
	value := Appearance(strings.TrimSpace(strings.ToLower(string(text))))
	switch value {
	case AutomaticAppearance, DarkAppearance, GreyDarkAppearance, GreyLightAppearance, LightAppearance:
		*self = value
		return nil
	default:
		return fmt.Errorf(
			"%q is not an appearance; write %q, %q, %q, %q, or %q",
			string(text), AutomaticAppearance, DarkAppearance, GreyDarkAppearance, GreyLightAppearance, LightAppearance,
		)
	}
}

func (self Appearance) On(background Background) Background {
	switch self {
	case DarkAppearance:
		return DarkBackground
	case GreyDarkAppearance:
		return GreyDarkBackground
	case GreyLightAppearance:
		return GreyLightBackground
	case LightAppearance:
		return LightBackground
	case AutomaticAppearance:
	}

	return background
}

type simulationGradient struct {
	from color.RGBA
	to   color.RGBA
}

var simulationGradients = map[Background]simulationGradient{
	DarkBackground:      {from: mustColour("#e6a8ff"), to: mustColour("#7ff0dd")},
	GreyDarkBackground:  {from: mustColour("#f2d0ff"), to: mustColour("#b0f4ea")},
	GreyLightBackground: {from: mustColour("#562670"), to: mustColour("#0e4741")},
	LightBackground:     {from: mustColour("#9b3fc4"), to: mustColour("#0b7c6d")},
}

func mustColour(code string) color.RGBA {
	value, isColour := colour(code)
	if !isColour {
		panic("not a colour: " + code)
	}

	return value
}

var recessions = map[Background]color.RGBA{
	DarkBackground:      {A: 0xff},
	GreyDarkBackground:  mustColour("#505050"),
	GreyLightBackground: mustColour("#a0a0a0"),
	LightBackground:     mustColour("#ffffff"),
}

func Recede(value color.RGBA) color.RGBA {
	toward := activeTheme.Load().recession

	return color.RGBA{
		R: midpoint(value.R, toward.R),
		G: midpoint(value.G, toward.G),
		B: midpoint(value.B, toward.B),
		A: value.A,
	}
}

func midpoint(first uint8, second uint8) uint8 {
	return uint8((uint16(first) + uint16(second)) / 2) //nolint:gosec // the mean of two bytes is a byte
}
