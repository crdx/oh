package style

import (
	"image/color"
	"sync/atomic"
)

type Theme struct {
	Normal         Paint `toml:"normal"`
	Dim            Paint `toml:"dim"`
	Accent         Paint `toml:"accent"`
	StatusSuccess  Paint `toml:"status_success"`
	StatusInfo     Paint `toml:"status_info"`
	StatusWarning  Paint `toml:"status_warning"`
	StatusDanger   Paint `toml:"status_danger"`
	SyntaxType     Paint `toml:"syntax_type"`
	SyntaxLiteral  Paint `toml:"syntax_literal"`
	SyntaxOperator Paint `toml:"syntax_operator"`
	SyntaxKeyword  Paint `toml:"syntax_keyword"`
	Skill          Paint `toml:"skill"`
	User           Paint `toml:"user"`
	Harness        Paint `toml:"harness"`
}

var defaultTheme = Theme{
	Normal:         "",
	Dim:            "#969896",
	Accent:         "#c08050",
	StatusSuccess:  "#4c9a2c",
	StatusInfo:     "#81a2be",
	StatusWarning:  "#cfad00",
	StatusDanger:   "#cc6666",
	SyntaxType:     "#f0c674",
	SyntaxLiteral:  "#b5bd68",
	SyntaxOperator: "#8abeb7",
	SyntaxKeyword:  "#c9a6d4",
	Skill:          "#c9a6d4",
	User:           "#343541",
	Harness:        "#303a43",
}

func DefaultTheme() Theme {
	return defaultTheme
}

type compiledTheme struct {
	normal         string
	dim            string
	accent         string
	statusWarning  string
	statusSuccess  string
	statusInfo     string
	statusDanger   string
	syntaxType     string
	syntaxLiteral  string
	syntaxOperator string
	syntaxKeyword  string
	skill          string
	user           string
	harness        string

	dimColour           color.RGBA
	statusWarningColour color.RGBA
	statusInfoColour    color.RGBA
	statusDangerColour  color.RGBA
}

var activeTheme = newAtomicTheme(defaultTheme)

func newAtomicTheme(theme Theme) *atomic.Pointer[compiledTheme] {
	active := &atomic.Pointer[compiledTheme]{}
	active.Store(compileTheme(theme))
	return active
}

func ApplyTheme(theme Theme) func() {
	previous := activeTheme.Swap(compileTheme(theme))

	return func() { activeTheme.Store(previous) }
}

func compileTheme(theme Theme) *compiledTheme {
	dimColour := graphicColour(theme.Dim, defaultTheme.Dim)
	statusWarningColour := graphicColour(theme.StatusWarning, defaultTheme.StatusWarning)
	statusInfoColour := graphicColour(theme.StatusInfo, defaultTheme.StatusInfo)
	statusDangerColour := graphicColour(theme.StatusDanger, defaultTheme.StatusDanger)

	return &compiledTheme{
		normal:              foregroundSequence(theme.Normal),
		dim:                 foregroundSequence(theme.Dim),
		accent:              foregroundSequence(theme.Accent),
		statusWarning:       foregroundSequence(theme.StatusWarning),
		statusSuccess:       foregroundSequence(theme.StatusSuccess),
		statusInfo:          foregroundSequence(theme.StatusInfo),
		statusDanger:        foregroundSequence(theme.StatusDanger),
		syntaxType:          foregroundSequence(theme.SyntaxType),
		syntaxLiteral:       foregroundSequence(theme.SyntaxLiteral),
		syntaxOperator:      foregroundSequence(theme.SyntaxOperator),
		syntaxKeyword:       foregroundSequence(theme.SyntaxKeyword),
		skill:               foregroundSequence(theme.Skill),
		user:                backgroundSequence(theme.User),
		harness:             backgroundSequence(theme.Harness),
		dimColour:           dimColour,
		statusWarningColour: statusWarningColour,
		statusInfoColour:    statusInfoColour,
		statusDangerColour:  statusDangerColour,
	}
}

func graphicColour(value Paint, fallback Paint) color.RGBA {
	if plan, err := parsePaint(string(value)); err == nil && plan.hasColour {
		return plan.colour
	}

	fallbackPlan, _ := parsePaint(string(fallback))

	return fallbackPlan.colour
}

func DimColour() color.RGBA {
	return activeTheme.Load().dimColour
}

func ChangeColour() color.RGBA {
	return activeTheme.Load().statusWarningColour
}

func InformationColour() color.RGBA {
	return activeTheme.Load().statusInfoColour
}

func FailureColour() color.RGBA {
	return activeTheme.Load().statusDangerColour
}
