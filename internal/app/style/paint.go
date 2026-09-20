package style

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultColour = "default"

	foregroundLayer = "38"
	backgroundLayer = "48"
	underlineLayer  = "58"

	underlinePrefix = underlineDecoration + ":"
)

const (
	boldDecoration          = "bold"
	faintDecoration         = "faint"
	italicDecoration        = "italic"
	underlineDecoration     = "underline"
	blinkDecoration         = "blink"
	reverseDecoration       = "reverse"
	hiddenDecoration        = "hidden"
	strikethroughDecoration = "strikethrough"
	overlineDecoration      = "overline"
)

const (
	italicCode    = "3"
	underlineCode = "4"
)

type decoration struct {
	name string
	code string
}

var decorations = []decoration{
	{name: boldDecoration, code: "1"},
	{name: faintDecoration, code: "2"},
	{name: italicDecoration, code: italicCode},
	{name: underlineDecoration, code: underlineCode},
	{name: blinkDecoration, code: "5"},
	{name: reverseDecoration, code: "7"},
	{name: hiddenDecoration, code: "8"},
	{name: strikethroughDecoration, code: "9"},
	{name: overlineDecoration, code: "53"},
}

var underlineShapes = map[string]string{
	"single": underlineCode,
	"double": underlineCode + ":2",
	"curly":  underlineCode + ":3",
	"dotted": underlineCode + ":4",
	"dashed": underlineCode + ":5",
}

type Paint string

func (self *Paint) UnmarshalText(text []byte) error {
	var words []string

	for word := range strings.FieldsSeq(strings.ToLower(string(text))) {
		if word != defaultColour {
			words = append(words, word)
		}
	}

	value := strings.Join(words, " ")
	if _, err := parsePaint(value); err != nil {
		return err
	}

	*self = Paint(value)

	return nil
}

type paintPlan struct {
	colour    color.RGBA
	hasColour bool

	chosenDecorations map[string]bool

	underlineShape     string
	underlineColour    color.RGBA
	hasUnderlineColour bool
}

func parsePaint(value string) (paintPlan, error) {
	plan := paintPlan{chosenDecorations: map[string]bool{}}

	for word := range strings.FieldsSeq(value) {
		if word == defaultColour {
			continue
		}

		if isDecoration(word) {
			plan.chosenDecorations[word] = true
			continue
		}

		if shape, isUnderline := strings.CutPrefix(word, underlinePrefix); isUnderline {
			if err := plan.takeUnderline(shape); err != nil {
				return paintPlan{}, err
			}
			continue
		}

		parsedColour, isColour := colour(word)
		if !isColour {
			return paintPlan{}, fmt.Errorf(
				"%q is not a colour or a decoration; write #rrggbb, %q, %s, or a shaped or coloured underline such as %q",
				word, defaultColour, strings.Join(decorationNames(), ", "), underlinePrefix+"curly",
			)
		}
		if plan.hasColour {
			return paintPlan{}, fmt.Errorf("%q is a second colour; write only one", word)
		}

		plan.colour, plan.hasColour = parsedColour, true
	}

	return plan, nil
}

func (self *paintPlan) takeUnderline(shape string) error {
	self.chosenDecorations[underlineDecoration] = true

	if code, isShape := underlineShapes[shape]; isShape {
		self.underlineShape = code
		return nil
	}

	parsedColour, isColour := colour(shape)
	if !isColour {
		return fmt.Errorf(
			"%q is not an underline; write %s#rrggbb or one of %s",
			underlinePrefix+shape, underlinePrefix, strings.Join(underlineShapeNames(), ", "),
		)
	}

	self.underlineColour, self.hasUnderlineColour = parsedColour, true

	return nil
}

func (self *paintPlan) sequence(layer string) string {
	var codes []string

	for _, entry := range decorations {
		if !self.chosenDecorations[entry.name] {
			continue
		}

		if entry.name == underlineDecoration && self.underlineShape != "" {
			codes = append(codes, self.underlineShape)
			continue
		}

		codes = append(codes, entry.code)
	}

	if self.hasUnderlineColour {
		codes = append(codes, underlineLayer+";2;"+channels(self.underlineColour))
	}
	if self.hasColour {
		codes = append(codes, layer+";2;"+channels(self.colour))
	}

	return strings.Join(codes, ";")
}

func isDecoration(word string) bool {
	for _, entry := range decorations {
		if entry.name == word {
			return true
		}
	}

	return false
}

func decorationNames() []string {
	names := make([]string, 0, len(decorations))
	for _, entry := range decorations {
		names = append(names, strconv.Quote(entry.name))
	}

	return names
}

func underlineShapeNames() []string {
	names := make([]string, 0, len(underlineShapes))
	for shape := range underlineShapes {
		names = append(names, strconv.Quote(shape))
	}
	slices.Sort(names)

	return names
}

func foregroundSequence(value Paint) string {
	return paintSequence(value, foregroundLayer)
}

func backgroundSequence(value Paint) string {
	return paintSequence(value, backgroundLayer)
}

func paintSequence(value Paint, layer string) string {
	plan, err := parsePaint(string(value))
	if err != nil {
		return ""
	}

	return plan.sequence(layer)
}
