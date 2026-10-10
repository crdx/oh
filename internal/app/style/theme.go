package style

import (
	"errors"
	"image/color"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

type Theme struct {
	Appearance Appearance `toml:"appearance"`
	Dark       Palette    `toml:"dark"`
	GreyDark   Palette    `toml:"grey-dark"`
	GreyLight  Palette    `toml:"grey-light"`
	Light      Palette    `toml:"light"`
	Tool       ToolTheme  `toml:"tool"`
}

type Palette struct {
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

func (self Theme) PaletteOn(background Background) Palette {
	switch self.Appearance.On(background) {
	case GreyDarkBackground:
		return self.GreyDark
	case GreyLightBackground:
		return self.GreyLight
	case LightBackground:
		return self.Light
	case DarkBackground:
	}

	return self.Dark
}

type ToolAppearance struct {
	Name  ToolName  `toml:"name"`
	Paint ToolPaint `toml:"paint"`
	Focus ToolPaint `toml:"focus"`
}

type ToolName string

func (self *ToolName) UnmarshalText(text []byte) error {
	value := strings.TrimSpace(string(text))
	if value == "" {
		return errors.New("tool name is empty")
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return errors.New("tool name contains a control character")
	}
	*self = ToolName(value)
	return nil
}

type ToolPaint string

var toolPaintRoles = map[string]bool{
	"normal":         true,
	"dim":            true,
	"accent":         true,
	"status_success": true,
	"status_info":    true,
	"status_warning": true,
	"status_danger":  true,
	"skill":          true,
}

func (self *ToolPaint) UnmarshalText(text []byte) error {
	value := strings.TrimSpace(strings.ToLower(string(text)))
	if toolPaintRoles[value] {
		*self = ToolPaint(value)
		return nil
	}
	var paint Paint
	if err := paint.UnmarshalText([]byte(value)); err != nil {
		return err
	}
	if paint == "" {
		*self = "default"
		return nil
	}
	*self = ToolPaint(paint)
	return nil
}

var defaultTheme = Theme{
	Appearance: AutomaticAppearance,
	Dark: Palette{
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
	},
	GreyDark: Palette{
		Normal:         "",
		Dim:            "#cfcfcf",
		Accent:         "#ffc49a",
		StatusSuccess:  "#b4f09c",
		StatusInfo:     "#bcdcff",
		StatusWarning:  "#ffe27a",
		StatusDanger:   "#ffb8b8",
		SyntaxType:     "#ffe9a8",
		SyntaxLiteral:  "#e2f0a0",
		SyntaxOperator: "#b0f4ea",
		SyntaxKeyword:  "#f2d0ff",
		Skill:          "#f2d0ff",
		User:           "#46464e",
		Harness:        "#424b52",
	},
	GreyLight: Palette{
		Normal:         "",
		Dim:            "#383b3e",
		Accent:         "#6a3210",
		StatusSuccess:  "#1f4a0f",
		StatusInfo:     "#1c3d63",
		StatusWarning:  "#563f00",
		StatusDanger:   "#761616",
		SyntaxType:     "#573a00",
		SyntaxLiteral:  "#3a4700",
		SyntaxOperator: "#0e4741",
		SyntaxKeyword:  "#562670",
		Skill:          "#562670",
		User:           "#c4c4cc",
		Harness:        "#bec8d0",
	},
	Light: Palette{
		Normal:         "",
		Dim:            "#6e7175",
		Accent:         "#a65d2e",
		StatusSuccess:  "#3a7d1f",
		StatusInfo:     "#3d6d9a",
		StatusWarning:  "#8f6a00",
		StatusDanger:   "#b83c3c",
		SyntaxType:     "#8a5f00",
		SyntaxLiteral:  "#5f7300",
		SyntaxOperator: "#2c7870",
		SyntaxKeyword:  "#8a4fa3",
		Skill:          "#8a4fa3",
		User:           "#e8e8ef",
		Harness:        "#e2eaf0",
	},
	Tool: ToolTheme{
		"read":  {Default: ToolAppearance{Name: "read"}},
		"skill": {Default: ToolAppearance{Name: "load", Paint: "skill", Focus: "skill"}},
		"ls":    {Default: ToolAppearance{Name: "ls"}},
		"find":  {Default: ToolAppearance{Name: "find"}},
		"grep":  {Default: ToolAppearance{Name: "grep"}},
		"write": {Default: ToolAppearance{Name: "write"}},
		"edit":  {Default: ToolAppearance{Name: "edit"}},
		"bash": {
			Default: ToolAppearance{Name: "$", Paint: "status_info"},
			Actions: map[string]ToolAppearance{
				"host_network": {Name: "$", Paint: "status_danger"},
			},
		},
		"job": {
			Default: ToolAppearance{Name: "job", Paint: "status_warning"},
			Actions: map[string]ToolAppearance{
				"start":   {},
				"stop":    {},
				"restart": {},
				"discard": {},
				"prune":   {},
				"status":  {Paint: "normal"},
				"output":  {Name: "cat", Paint: "normal"},
				"list":    {Paint: "normal"},
			},
		},
		"subagent": {
			Default: ToolAppearance{Name: "subagent", Paint: "status_warning"},
			Actions: map[string]ToolAppearance{
				"start":  {Name: "spawn"},
				"send":   {},
				"stop":   {},
				"status": {Paint: "normal"},
				"output": {Name: "cat", Paint: "normal"},
				"list":   {Paint: "normal"},
			},
		},
		"wait": {Default: ToolAppearance{Name: "wait", Paint: "normal"}},
		"forward": {
			Default: ToolAppearance{Name: "forward", Paint: "status_warning"},
			Actions: map[string]ToolAppearance{
				"add":    {Name: "forward"},
				"remove": {Name: "close"},
				"list":   {Paint: "normal"},
			},
		},
		"lookup": {Default: ToolAppearance{Name: "lookup", Paint: "status_info"}},
		"fetch":  {Default: ToolAppearance{Name: "fetch", Paint: "status_info"}},
		"notify": {Default: ToolAppearance{Name: "notify"}},
		"title":  {Default: ToolAppearance{Name: "title"}},
	},
}

func DefaultTheme() Theme {
	return cloneTheme(defaultTheme)
}

func cloneTheme(theme Theme) Theme {
	theme.Tool = theme.Tool.Clone()
	return theme
}

func (self Theme) Equal(other Theme) bool {
	return reflect.DeepEqual(self, other)
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
	tool           map[string]compiledToolAppearance

	dimColour           color.RGBA
	statusWarningColour color.RGBA
	statusInfoColour    color.RGBA
	statusDangerColour  color.RGBA

	simulation simulationGradient
	recession  color.RGBA
}

type compiledToolAppearance struct {
	name       string
	paint      string
	focusPaint string
	hasPaint   bool
	hasFocus   bool
}

type appliedTheme struct {
	theme         Theme
	background    Background
	compiledValue *compiledTheme
}

var activeTheme = newAtomicTheme(defaultTheme, DarkBackground)

func newAtomicTheme(theme Theme, background Background) *atomic.Pointer[compiledTheme] {
	active := &atomic.Pointer[compiledTheme]{}
	active.Store(compileTheme(theme, background))
	return active
}

var themeInUse = struct {
	mutex      sync.Mutex
	theme      Theme
	background Background
}{theme: defaultTheme, background: DarkBackground}

func ApplyTheme(theme Theme) func() {
	themeInUse.mutex.Lock()
	defer themeInUse.mutex.Unlock()

	return swapTheme(theme, themeInUse.background)
}

func ReportBackground(background Background) func() {
	themeInUse.mutex.Lock()
	defer themeInUse.mutex.Unlock()

	return swapTheme(themeInUse.theme, background)
}

func swapTheme(theme Theme, background Background) func() {
	previous := appliedTheme{theme: themeInUse.theme, background: themeInUse.background}
	previous.compiledValue = activeTheme.Swap(compileTheme(theme, background))
	themeInUse.theme = cloneTheme(theme)
	themeInUse.background = background

	return func() {
		themeInUse.mutex.Lock()
		defer themeInUse.mutex.Unlock()

		activeTheme.Store(previous.compiledValue)
		themeInUse.theme = previous.theme
		themeInUse.background = previous.background
	}
}

func compileTheme(theme Theme, background Background) *compiledTheme {
	chosenBackground := theme.Appearance.On(background)
	palette := theme.PaletteOn(background)
	fallback := defaultTheme.PaletteOn(chosenBackground)

	dimColour := graphicColour(palette.Dim, fallback.Dim)
	statusWarningColour := graphicColour(palette.StatusWarning, fallback.StatusWarning)
	statusInfoColour := graphicColour(palette.StatusInfo, fallback.StatusInfo)
	statusDangerColour := graphicColour(palette.StatusDanger, fallback.StatusDanger)

	compiledThemeValue := &compiledTheme{
		normal:              foregroundSequence(palette.Normal),
		dim:                 foregroundSequence(palette.Dim),
		accent:              foregroundSequence(palette.Accent),
		statusWarning:       foregroundSequence(palette.StatusWarning),
		statusSuccess:       foregroundSequence(palette.StatusSuccess),
		statusInfo:          foregroundSequence(palette.StatusInfo),
		statusDanger:        foregroundSequence(palette.StatusDanger),
		syntaxType:          foregroundSequence(palette.SyntaxType),
		syntaxLiteral:       foregroundSequence(palette.SyntaxLiteral),
		syntaxOperator:      foregroundSequence(palette.SyntaxOperator),
		syntaxKeyword:       foregroundSequence(palette.SyntaxKeyword),
		skill:               foregroundSequence(palette.Skill),
		user:                backgroundSequence(palette.User),
		harness:             backgroundSequence(palette.Harness),
		dimColour:           dimColour,
		statusWarningColour: statusWarningColour,
		statusInfoColour:    statusInfoColour,
		statusDangerColour:  statusDangerColour,
		simulation:          simulationGradients[chosenBackground],
		recession:           recessions[chosenBackground],
		tool:                make(map[string]compiledToolAppearance),
	}
	for kind, configuredAppearance := range theme.Tool.Resolved() {
		appearance := compiledToolAppearance{name: string(configuredAppearance.Name)}
		if configuredAppearance.Paint != "" {
			appearance.paint = resolveToolPaint(compiledThemeValue, configuredAppearance.Paint)
			appearance.hasPaint = true
		}
		if configuredAppearance.Focus != "" {
			appearance.focusPaint = resolveToolPaint(compiledThemeValue, configuredAppearance.Focus)
			appearance.hasFocus = true
		}
		compiledThemeValue.tool[kind] = appearance
	}
	return compiledThemeValue
}

func resolveToolPaint(theme *compiledTheme, paint ToolPaint) string {
	switch string(paint) {
	case "normal":
		return theme.normal
	case "dim":
		return theme.dim
	case "accent":
		return theme.accent
	case "status_success":
		return theme.statusSuccess
	case "status_info":
		return theme.statusInfo
	case "status_warning":
		return theme.statusWarning
	case "status_danger":
		return theme.statusDanger
	case "skill":
		return theme.skill
	default:
		return foregroundSequence(Paint(paint))
	}
}

func ToolCallAppearance(kind string, fallbackName string, isReadOnly bool) (string, Style, Style) {
	appearance := activeTheme.Load().tool[kind]
	name := appearance.name
	if name == "" {
		name = fallbackName
	}
	nameStyle := themedForeground(func(theme *compiledTheme) string {
		if configuredAppearance, isKnown := theme.tool[kind]; isKnown && configuredAppearance.hasPaint {
			return configuredAppearance.paint
		}
		if isReadOnly {
			return theme.normal
		}
		return theme.statusWarning
	})
	focusStyle := themedForeground(func(theme *compiledTheme) string {
		if configuredAppearance, isKnown := theme.tool[kind]; isKnown && configuredAppearance.hasFocus {
			return configuredAppearance.focusPaint
		}
		return theme.accent
	})
	return name, nameStyle, focusStyle
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
