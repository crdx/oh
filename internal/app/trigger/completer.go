package trigger

import (
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
	sources  map[rune]Source
	dropdown dropdown.Dropdown
	current  Word
	results  []Result
	limit    int
	total    int
	changes  chan struct{}
}

func New(sources ...Source) *Completer {
	self := &Completer{
		sources: make(map[rune]Source, len(sources)),
		changes: make(chan struct{}, 1),
	}
	for _, source := range sources {
		self.sources[source.Symbol()] = source
	}

	return self
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
			labels := self.fetch()
			self.dropdown.ExtendOptions(labels, self.total)
		}
		return true

	case dropdown.Swallowed:
		return true

	case dropdown.Chosen:
		result, _ := self.Selected()
		word := self.current
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
	word, isFound := self.find(editor)
	if !isFound {
		return false
	}

	source := self.sources[word.Symbol]
	self.close()
	self.dropdown.SetElision(source.Elision())
	self.dropdown.Open()
	self.current = word
	source.Open(self.announceChange)
	self.match()

	return true
}

func (self *Completer) Typed(editor Editor, keypress key.Key) bool {
	if keypress.Code != key.Rune || keypress.Mod&^key.Shift != 0 || !self.isSymbol(keypress.Value) {
		return false
	}

	word, isFound := self.find(editor)
	if !isFound || word.Start != editor.Cursor()-1 || word.Symbol != keypress.Value {
		return false
	}

	return self.Open(editor)
}

func (self *Completer) Sync(editor Editor) {
	if !self.dropdown.IsOpen() {
		return
	}

	word, isFound := self.find(editor)
	if !isFound || word.Symbol != self.current.Symbol || word.Start != self.current.Start {
		self.close()
		return
	}

	isUnchanged := word.Query == self.current.Query
	self.current = word
	if !isUnchanged {
		self.match()
	}
}

func (self *Completer) Receive() {
	if self.dropdown.IsOpen() {
		self.match()
	}
}

func (self *Completer) find(editor Editor) (Word, bool) {
	if editor.IsSearching() {
		return Word{}, false
	}

	return Find(editor.Runes(), editor.Cursor(), self.isSymbol)
}

func (self *Completer) isSymbol(symbol rune) bool {
	_, isFound := self.sources[symbol]
	return isFound
}

func (self *Completer) announceChange() {
	select {
	case self.changes <- struct{}{}:
	default:
	}
}

func (self *Completer) match() {
	self.limit = resultPage
	labels := self.fetch()
	self.dropdown.SetOptions(labels, self.total)
}

func (self *Completer) fetch() []string {
	results := self.sources[self.current.Symbol].Results(self.current.Query, self.limit)

	labels := make([]string, len(results.Items))
	for i, result := range results.Items {
		labels[i] = result.Text
	}

	self.results = results.Items
	self.total = results.Total
	self.dropdown.SetPlaceholder(results.Placeholder)

	return labels
}

func (self *Completer) close() {
	self.dropdown.Close()
	self.results = nil
}
