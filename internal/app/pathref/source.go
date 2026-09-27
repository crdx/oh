package pathref

import (
	"strings"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/trigger"
)

const (
	Symbol        = '@'
	listingText   = "listing files…"
	noMatchText   = "no matching paths"
	failurePrefix = "could not list files: "
)

type Source struct {
	index   *Index
	matcher Matcher
}

func NewSource(index *Index) *Source {
	return &Source{index: index}
}

func (self *Source) Symbol() rune {
	return Symbol
}

func (self *Source) Elision() dropdown.Elision {
	return dropdown.ElideStart
}

func (self *Source) Find(runes []rune, cursor int) (trigger.Word, bool) {
	return trigger.FindWord(runes, cursor, Symbol)
}

func (self *Source) Open(announceChange func()) {
	self.index.Refresh(announceChange)
}

func (self *Source) Results(word trigger.Word, limit int) trigger.Results {
	listing := self.index.Listing()
	if listing == nil {
		return trigger.Results{Placeholder: listingText}
	}

	placeholder := noMatchText
	if listing.Err != nil {
		placeholder = failurePrefix + listing.Err.Error()
	}

	paths, total := self.matcher.Match(listing, word.Query, limit)
	items := make([]trigger.Result, len(paths))
	for i, path := range paths {
		isOpenEnded := IsDirectory(path)
		items[i] = trigger.Result{
			Label:       path,
			Text:        trigger.WordText(Symbol, path, word.IsQuoted, isOpenEnded),
			IsOpenEnded: isOpenEnded,
		}
	}

	return trigger.Results{Items: items, Total: total, Placeholder: placeholder}
}

func IsDirectory(path string) bool {
	return strings.HasSuffix(path, "/")
}
