package style

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"

	"crdx.org/col"

	"crdx.org/oh/internal/app/escape"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
)

type Style func(format any, args ...any) string

const (
	reset  = "\x1b[0m"
	orchid = "#e6a8ff"
	aqua   = "#7ff0dd"
)

var (
	Accent Style = accent()
	Normal Style = normal()
	Dim    Style = dim()

	Answer     Style = Normal
	Call       Style = Normal
	TypedInput Style = Normal

	PreviewLoadHint    Style = decorate(col.Italic, decorate(col.Dim, Success))
	PreviewRunningHint Style = decorate(col.Italic, decorate(col.Dim, Change))
	Reasoning          Style = decorate(col.Italic, Dim)
	RunningSession     Style = decorate(col.Italic, Dim)
	Column             Style = decorate(col.Underline, Dim)
	Greeting           Style = col.Italic
	PendingPrefix      Style = col.Underline

	Success Style = success()
	Read    Style = success()

	Change      Style = warning()
	Warning     Style = warning()
	Write       Style = warning()
	StoppedTurn Style = warning()

	Info         Style = information()
	Shell        Style = information()
	Network      Style = information()
	Lookup       Style = information()
	Git          Style = information()
	ScratchAlias Style = decorate(col.Italic, Info)

	Failure Style = danger()
	Hazard  Style = danger()

	LowPrice    Style = success()
	HighPrice   Style = warning()
	MediumPrice Style = information()
	WtfPrice    Style = danger()

	CancelledCall Style = Dim
	Qualifier     Style = Dim
	Result        Style = Dim
	Rule          Style = Dim
	Subtle        Style = Dim
	ScrolledInput Style = Dim

	Harness Style = harnessBackground()
	User    Style = userBackground()

	Skill Style = skill()

	Subject Style = accent()
	Spinner Style = accent()
	Prompt  Style = accent()

	ChosenRow            Style = accent()
	ChosenRunningSession Style = decorate(col.Italic, ChosenRow)

	Simulation Style = gradient(orchid, aqua)

	Heading         Style = warning()
	MarkdownHeading Style = decorate(col.Bold, Heading)

	Link    Style = information()
	Address Style = Dim
	Code    Style = accent()
	Block   Style = Dim
	Quote   Style = Dim
	Bullet  Style = accent()
	Border  Style = Dim

	Comment     Style = Dim
	Keyword     Style = syntaxKeyword()
	Function    Style = information()
	Literal     Style = syntaxLiteral()
	Number      Style = accent()
	Type        Style = syntaxType()
	Operator    Style = syntaxOperator()
	Variable    Style = normal()
	Punctuation Style = normal()

	InsertedText Style = success()
	DeletedText  Style = danger()
	Hunk         Style = information()
)

func ExecWhenWritable(isWritable bool) Style {
	if isWritable {
		return Write
	}

	return Read
}

var isColorEnabled = true

func Init(screen any) func() {
	previous := isColorEnabled

	apply(os.Getenv("NO_COLOR") == "" && tty.Is(screen))

	return func() {
		apply(previous)
	}
}

func apply(isEnabled bool) {
	isColorEnabled = isEnabled

	if isEnabled {
		col.Enable()
	} else {
		col.Disable()
	}
}

func (self Style) Over(text string) string {
	const marker = "\x00"

	openingSequence, closingSequence, found := strings.Cut(self(marker), marker)
	if !found || openingSequence == "" {
		return text
	}

	resumedText := strings.TrimSuffix(strings.ReplaceAll(text, reset, reset+openingSequence), openingSequence)
	if strings.HasSuffix(resumedText, closingSequence) {
		return openingSequence + resumedText
	}

	return openingSequence + resumedText + closingSequence
}

func (self Style) Join(parts ...string) string {
	joinedText := util.JoinNonEmpty(parts...)
	if joinedText == "" {
		return ""
	}

	return self(joinedText)
}

func Width(text string) int {
	return width.Of(text)
}

func Error(err error) string {
	lines := strings.Split(err.Error(), "\n")
	for i, line := range lines {
		lines[i] = strutil.CapitaliseSentence(line)
		if i > 0 {
			lines[i] = "  " + lines[i]
		}
	}

	return Failure("✗ " + strings.Join(lines, "\n"))
}

func Plain(text string) string {
	var out strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' {
			sequence := escape.GetSequence(runes, i)
			out.WriteString(sequence.Text)
			i = sequence.End
			continue
		}

		out.WriteRune(runes[i])
		i++
	}

	return out.String()
}

func Quantity(text string) string {
	var out strings.Builder

	for start := 0; start < len(text); {
		end := start + 1
		for end < len(text) && isNumeric(text[end]) == isNumeric(text[start]) {
			end++
		}

		run := text[start:end]
		if isNumeric(text[start]) {
			out.WriteString(Normal(run))
		} else {
			out.WriteString(Subtle(run))
		}

		start = end
	}

	return out.String()
}

func isNumeric(character byte) bool {
	return character >= '0' && character <= '9' || character == '.' || character == '?'
}

func decorate(decoration Style, inner Style) Style {
	return func(format any, args ...any) string {
		return decoration(inner(format, args...))
	}
}

func gradient(from string, to string) Style {
	first, hasFirst := colour(from)
	last, hasLast := colour(to)

	return func(format any, args ...any) string {
		text := fmt.Sprint(format)

		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}

		if !isColorEnabled || !hasFirst || !hasLast {
			return text
		}

		var paint strings.Builder

		runes := []rune(text)
		for i, character := range runes {
			paint.WriteString("\x1b[")
			paint.WriteString(italicCode)
			paint.WriteString(";")
			paint.WriteString(sequenceFor(blend(first, last, i, len(runes))))
			paint.WriteString("m")
			paint.WriteRune(character)
			paint.WriteString(reset)
		}

		return paint.String()
	}
}

func blend(from color.RGBA, to color.RGBA, step int, steps int) color.RGBA {
	if steps <= 1 {
		return from
	}

	along := float64(step) / float64(steps-1)

	return color.RGBA{
		R: channelAlong(from.R, to.R, along),
		G: channelAlong(from.G, to.G, along),
		B: channelAlong(from.B, to.B, along),
		A: 0xff,
	}
}

func channelAlong(from uint8, to uint8, along float64) uint8 {
	return uint8(float64(from) + (float64(to)-float64(from))*along)
}

func normal() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.normal })
}

func dim() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.dim })
}

func accent() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.accent })
}

func success() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.statusSuccess })
}

func information() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.statusInfo })
}

func warning() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.statusWarning })
}

func danger() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.statusDanger })
}

func syntaxType() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.syntaxType })
}

func syntaxLiteral() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.syntaxLiteral })
}

func syntaxOperator() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.syntaxOperator })
}

func syntaxKeyword() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.syntaxKeyword })
}

func skill() Style {
	return themedForeground(func(theme *compiledTheme) string { return theme.skill })
}

func userBackground() Style {
	return themedBackground(func(theme *compiledTheme) string { return theme.user })
}

func harnessBackground() Style {
	return themedBackground(func(theme *compiledTheme) string { return theme.harness })
}

func themedForeground(selectCode func(*compiledTheme) string) Style {
	return func(format any, args ...any) string {
		text := fmt.Sprint(format)

		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}

		code := selectCode(activeTheme.Load())
		if code == "" || !isColorEnabled {
			return text
		}

		return "\x1b[" + code + "m" + text + reset
	}
}

func themedBackground(selectCode func(*compiledTheme) string) Style {
	return func(format any, args ...any) string {
		text := fmt.Sprint(format)

		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}

		code := selectCode(activeTheme.Load())
		if code == "" || !isColorEnabled {
			return text
		}

		openingSequence := "\x1b[" + code + "m"
		text = strings.ReplaceAll(text, reset, reset+openingSequence)

		return openingSequence + text + reset
	}
}

func sequenceFor(value color.RGBA) string {
	return foregroundLayer + ";2;" + channels(value)
}

func channels(value color.RGBA) string {
	return fmt.Sprintf("%d;%d;%d", value.R, value.G, value.B)
}

func colour(value string) (color.RGBA, bool) {
	if len(value) != len("#rrggbb") || value[0] != '#' {
		return color.RGBA{}, false
	}

	colourValue := color.RGBA{A: 0xff}

	for i, channel := range []*uint8{&colourValue.R, &colourValue.G, &colourValue.B} {
		at := 1 + i*2

		channelValue, err := strconv.ParseUint(value[at:at+2], 16, 8)
		if err != nil {
			return color.RGBA{}, false
		}

		*channel = uint8(channelValue)
	}

	return colourValue, true
}
