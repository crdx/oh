package trigger

import (
	"strings"
	"unicode"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/key"
)

const resultPage = 200

type Editor interface {
	Runes() []rune
	Cursor() int
	Replace(start int, end int, text string)
	IsSearching() bool
}

type Completer struct {
	sources  []Source
	source   Source
	dropdown dropdown.Dropdown
	current  Word
	results  []Result
	limit    int
	total    int
	changes  chan struct{}
}

func New(sources ...Source) *Completer {
	return &Completer{
		sources: sources,
		changes: make(chan struct{}, 1),
	}
}

func (self *Completer) Changes() <-chan struct{} {
	return self.changes
}

func (self *Completer) IsOpen() bool {
	return self.dropdown.IsOpen()
}

func (self *Completer) Selected() (Result, bool) {
	index, isSelected := self.dropdown.Selected()
	if !isSelected {
		return Result{}, false
	}

	return self.results[index], true
}

func (self *Completer) Rows(columns int, budget int) []string {
	return self.dropdown.Rows(columns, budget)
}

func (self *Completer) Apply(editor Editor, keypress key.Key) bool {
	switch self.dropdown.Apply(keypress) {
	case dropdown.Moved:
		if self.dropdown.IsShortOfOptions() {
			self.limit += resultPage
			options := self.fetch()
			self.dropdown.ExtendOptions(options, self.total)
		}
		return true

	case dropdown.Swallowed:
		return true

	case dropdown.Chosen:
		result, _ := self.Selected()
		word := self.current
		if keypress.Code == key.Enter && self.offersOnlyAsWritten(string(editor.Runes()[word.Start:word.End])) {
			self.close()
			return false
		}
		editor.Replace(word.Start, word.End, Replacement(word, result, editor.Runes()))
		if result.IsOpenEnded {
			self.Sync(editor)
		} else {
			self.close()
		}
		return true

	case dropdown.Dismissed:
		self.close()
		return true

	case dropdown.Ignored:
	}

	return false
}

func (self *Completer) Open(editor Editor) bool {
	for _, source := range self.sources {
		if word, isFound := self.find(source, editor); isFound {
			self.openWith(source, word)
			return true
		}
	}

	return false
}

func (self *Completer) Typed(editor Editor, keypress key.Key) bool {
	if keypress.Code != key.Rune || keypress.Mod&^key.Shift != 0 {
		return false
	}

	for _, source := range self.sources {
		if source.Symbol() != keypress.Value {
			continue
		}
		if word, isFound := self.find(source, editor); isFound && word.Start == editor.Cursor()-1 {
			self.openWith(source, word)
			return true
		}
	}

	return false
}

func (self *Completer) Sync(editor Editor) {
	if !self.dropdown.IsOpen() {
		return
	}

	word, isFound := self.find(self.source, editor)
	if !isFound || word.Start != self.current.Start {
		self.close()
		return
	}

	isUnchanged := word.Query == self.current.Query && word.IsQuoted == self.current.IsQuoted
	self.current = word
	if isUnchanged {
		return
	}

	self.match()
	if self.offersOnly(string(editor.Runes()[word.Start:word.End])) {
		self.close()
	}
}

func (self *Completer) Receive() {
	if self.dropdown.IsOpen() {
		self.match()
	}
}

func (self *Completer) offersOnly(text string) bool {
	return self.offersOne() && self.results[0].Text == text
}

func (self *Completer) offersOnlyAsWritten(text string) bool {
	return self.offersOne() && strings.TrimRightFunc(self.results[0].Text, unicode.IsSpace) == text
}

func (self *Completer) offersOne() bool {
	return self.total == 1 && len(self.results) == 1
}

func (self *Completer) openWith(source Source, word Word) {
	self.close()
	self.source = source
	self.current = word
	self.dropdown.SetElision(source.Elision())
	self.dropdown.Open()
	source.Open(self.announceChange)
	self.match()
}

func (self *Completer) find(source Source, editor Editor) (Word, bool) {
	if editor.IsSearching() {
		return Word{}, false
	}

	return source.Find(editor.Runes(), editor.Cursor())
}

func (self *Completer) announceChange() {
	select {
	case self.changes <- struct{}{}:
	default:
	}
}

func (self *Completer) match() {
	self.limit = resultPage
	options := self.fetch()
	self.dropdown.SetOptions(options, self.total)
}

func (self *Completer) fetch() []dropdown.Option {
	results := self.source.Results(self.current, self.limit)

	options := make([]dropdown.Option, len(results.Items))
	for i, result := range results.Items {
		options[i] = dropdown.Option{Label: result.Label, Detail: result.Detail}
	}

	self.results = results.Items
	self.total = results.Total
	self.dropdown.SetPlaceholder(results.Placeholder)

	return options
}

func (self *Completer) close() {
	self.dropdown.Close()
	self.results = nil
}
