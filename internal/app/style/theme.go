package style

import (
	"errors"
	"image/color"
	"maps"
	"reflect"
	"strings"
	"sync/atomic"
	"unicode"
)

type Theme struct {
	Normal         Paint                     `toml:"normal"`
	Dim            Paint                     `toml:"dim"`
	Accent         Paint                     `toml:"accent"`
	StatusSuccess  Paint                     `toml:"status_success"`
	StatusInfo     Paint                     `toml:"status_info"`
	StatusWarning  Paint                     `toml:"status_warning"`
	StatusDanger   Paint                     `toml:"status_danger"`
	SyntaxType     Paint                     `toml:"syntax_type"`
	SyntaxLiteral  Paint                     `toml:"syntax_literal"`
	SyntaxOperator Paint                     `toml:"syntax_operator"`
	SyntaxKeyword  Paint                     `toml:"syntax_keyword"`
	Skill          Paint                     `toml:"skill"`
	User           Paint                     `toml:"user"`
	Harness        Paint                     `toml:"harness"`
	Tool           map[string]ToolAppearance `toml:"tool"`
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
	Tool: map[string]ToolAppearance{
		"read":          {Name: "read"},
		"skill":         {Name: "load", Paint: "skill", Focus: "skill"},
		"ls":            {Name: "ls"},
		"find":          {Name: "find"},
		"grep":          {Name: "grep"},
		"write":         {Name: "write"},
		"edit":          {Name: "edit"},
		"bash":          {Name: "$", Paint: "status_info"},
		"job":           {Name: "job"},
		"job_start":     {Name: "start", Paint: "status_warning"},
		"job_restart":   {Name: "restart", Paint: "status_warning"},
		"job_status":    {Name: "status", Paint: "status_warning"},
		"job_output":    {Name: "output", Paint: "status_warning"},
		"job_wait_any":  {Name: "wait for", Paint: "status_warning"},
		"job_wait_all":  {Name: "wait for", Paint: "status_warning"},
		"job_stop":      {Name: "stop", Paint: "status_warning"},
		"job_list":      {Name: "list", Paint: "status_warning"},
		"job_discard":   {Name: "discard", Paint: "status_warning"},
		"job_prune":     {Name: "prune", Paint: "status_warning"},
		"expose":        {Name: "expose"},
		"expose_add":    {Name: "expose", Paint: "status_warning"},
		"expose_remove": {Name: "unexpose", Paint: "status_warning"},
		"expose_list":   {Name: "list", Paint: "status_warning"},
		"lookup":        {Name: "lookup", Paint: "status_info"},
		"fetch":         {Name: "fetch", Paint: "status_info"},
		"notify":        {Name: "notify"},
		"title":         {Name: "title"},
	},
}

func DefaultTheme() Theme {
	return cloneTheme(defaultTheme)
}

func cloneTheme(theme Theme) Theme {
	theme.Tool = maps.Clone(theme.Tool)
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
}

type compiledToolAppearance struct {
	name       string
	paint      string
	focusPaint string
	hasPaint   bool
	hasFocus   bool
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

	compiledThemeValue := &compiledTheme{
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
		tool:                make(map[string]compiledToolAppearance),
	}
	for kind, configuredAppearance := range theme.Tool {
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
