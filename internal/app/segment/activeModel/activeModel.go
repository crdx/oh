package activeModel

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/style"
)

const (
	filledSquare      = "▪"
	emptySquare       = "▫"
	unsupportedSquare = "·"
)

type Settings struct {
	Name         string
	Effort       string
	EffortLevels []string
	IsFast       bool
	IsSimulated  bool
}

type state struct {
	settings Settings
}

func New(settings Settings) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{settings: settings}, nil
	}
}

func (self state) Render(segment.Context) string {
	name := model.DisplayName(self.settings.Name)
	badge := self.paint(name[0])
	if len(name) > 1 {
		badge += " " + style.Subtle(name[1])
	}
	if self.settings.IsFast {
		badge = fastMode.GetMark(true) + " " + badge
	}

	squares := thinkingSquares(self.settings.Effort, self.settings.EffortLevels)
	if squares == "" {
		return badge
	}

	return badge + " " + styleThinkingSquares(squares)
}

func (self state) paint(name string) string {
	if self.settings.IsSimulated {
		return style.Simulation(name)
	}

	return style.Normal(name)
}

func thinkingSquares(effort string, effortLevels []string) string {
	ladder := model.EffortOrder[1:]

	if !slices.ContainsFunc(ladder, func(level string) bool { return slices.Contains(effortLevels, level) }) {
		return ""
	}

	var squares strings.Builder

	for _, level := range ladder {
		switch {
		case !slices.Contains(effortLevels, level):
			squares.WriteString(unsupportedSquare)
		case level == effort:
			squares.WriteString(filledSquare)
		default:
			squares.WriteString(emptySquare)
		}
	}

	return squares.String()
}

func styleThinkingSquares(squares string) string {
	var renderedText strings.Builder
	var subtle strings.Builder

	flushSubtle := func() {
		if subtle.Len() > 0 {
			renderedText.WriteString(style.Subtle(subtle.String()))
			subtle.Reset()
		}
	}

	for _, square := range squares {
		if string(square) == filledSquare {
			flushSubtle()
			renderedText.WriteString(style.ChosenRow(string(square)))
		} else {
			subtle.WriteRune(square)
		}
	}
	flushSubtle()

	return renderedText.String()
}
