package pathref

import (
	"strings"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/trigger"
)

const (
	Symbol             = '@'
	listingText        = "listing files…"
	ignoredListingText = "listing ignored files…"
	noMatchText        = "tab to search ignored"
	noIgnoredText      = "no matching ignored paths"
	failurePrefix      = "could not list files: "
)

type Source struct {
	index          *Index
	ignoredIndex   *Index
	matcher        Matcher
	announceChange func()
}

func NewSource(index *Index) *Source {
	ignoredIndex := NewIndexWith(index.directory, index.getExcludedNames, ListIgnoredFiles, index.now)
	return NewSourceWithIgnored(index, ignoredIndex)
}

func NewSourceWithIgnored(index *Index, ignoredIndex *Index) *Source {
	return &Source{index: index, ignoredIndex: ignoredIndex}
}

func (self *Source) Symbol() rune {
	return Symbol
}

func (self *Source) Elision() dropdown.Elision {
	return dropdown.ElideStart
}

func (self *Source) Find(runes []rune, cursor int) (trigger.Word, bool) {
	return trigger.FindWordWithMarker(runes, cursor, Symbol, '!')
}

func (self *Source) Open(announceChange func()) {
	self.announceChange = announceChange
	self.index.Refresh(announceChange)
}

func (self *Source) NoMatchTab(word trigger.Word, text string) (string, bool) {
	listing := self.index.Listing()
	if listing == nil || listing.Err != nil || strings.HasPrefix(word.Query, "!") {
		return "", false
	}
	return "@!" + strings.TrimPrefix(text, "@"), true
}

func (self *Source) Results(word trigger.Word, limit int) trigger.Results {
	index := self.index
	query := word.Query
	pendingText := listingText
	placeholder := noMatchText
	isIgnoredQuery := strings.HasPrefix(query, "!")
	if isIgnoredQuery {
		index = self.ignoredIndex
		query = strings.TrimPrefix(query, "!")
		pendingText = ignoredListingText
		placeholder = noIgnoredText
	}
	if self.announceChange != nil {
		index.Refresh(self.announceChange)
	}
	listing := index.Listing()
	if listing == nil {
		return trigger.Results{Placeholder: pendingText}
	}
	if listing.Err != nil {
		placeholder = failurePrefix + listing.Err.Error()
	}

	paths, total := self.matcher.Match(listing, query, limit)
	items := make([]trigger.Result, len(paths))
	for i, path := range paths {
		isOpenEnded := IsDirectory(path)
		text := trigger.WordText(Symbol, path, word.IsQuoted, isOpenEnded)
		if isIgnoredQuery && isOpenEnded {
			text = "@!" + trigger.QuotedText(path, word.IsQuoted, true)
		}
		items[i] = trigger.Result{
			Label:       path,
			Text:        text,
			IsOpenEnded: isOpenEnded,
		}
	}

	return trigger.Results{Items: items, Total: total, Placeholder: placeholder}
}

func IsDirectory(path string) bool {
	return strings.HasSuffix(path, "/")
}
