package slash

import (
	"strings"
	"unicode"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/trigger"
)

const (
	Symbol      = '/'
	noMatchText = "no matching commands"
)

type Source struct {
	getRegistry func() Registry
}

func NewSource(getRegistry func() Registry) *Source {
	return &Source{getRegistry: getRegistry}
}

func (self *Source) Symbol() rune {
	return Symbol
}

func (self *Source) Elision() dropdown.Elision {
	return dropdown.ElideEnd
}

func (self *Source) Find(runes []rune, cursor int) (trigger.Word, bool) {
	if len(runes) == 0 || runes[0] != Symbol || cursor < 1 || cursor > len(runes) {
		return trigger.Word{}, false
	}

	prefix := string(runes[:cursor])
	if !self.getRegistry().Completes(prefix) {
		return trigger.Word{}, false
	}

	end := cursor
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}

	return trigger.Word{Start: 0, End: end, Query: prefix}, true
}

func (self *Source) Open(func()) {}

func (self *Source) Results(word trigger.Word, limit int) trigger.Results {
	completions := self.getRegistry().Completions(word.Query)

	items := make([]trigger.Result, 0, min(len(completions), limit))
	for _, completion := range completions[:min(len(completions), limit)] {
		description, _, _ := strings.Cut(strings.TrimSpace(completion.Description), "\n")
		text := completion.Text
		if completion.TakesArguments {
			text += " "
		}

		items = append(items, trigger.Result{
			Label:       completion.Label,
			Detail:      description,
			Text:        text,
			IsOpenEnded: completion.TakesArguments,
			IsFinal:     completion.IsFinal,
		})
	}

	return trigger.Results{Items: items, Total: len(completions), Placeholder: noMatchText}
}
