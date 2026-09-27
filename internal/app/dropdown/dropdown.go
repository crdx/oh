package dropdown

import (
	"fmt"
	"strings"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const (
	MaxRows      = 8
	marker       = "› "
	markerIndent = "  "
)

type Outcome int

const (
	Ignored Outcome = iota
	Moved
	Swallowed
	Chosen
	Dismissed
)

type Elision int

const (
	ElideEnd Elision = iota
	ElideStart
)

type Dropdown struct {
	elision     Elision
	options     []string
	total       int
	placeholder string
	selection   int
	offset      int
	height      int
	isOpen      bool
}

func (self *Dropdown) Open() {
	self.isOpen = true
}

func (self *Dropdown) Close() {
	*self = Dropdown{elision: self.elision, placeholder: self.placeholder}
}

func (self *Dropdown) SetElision(elision Elision) {
	self.elision = elision
}

func (self *Dropdown) IsOpen() bool {
	return self.isOpen
}

func (self *Dropdown) SetOptions(options []string, total int) {
	self.options = options
	self.total = max(total, len(options))
	self.selection = 0
	self.offset = 0
	self.height = max(self.height, min(MaxRows, max(self.total, 1)))
}

func (self *Dropdown) ExtendOptions(options []string, total int) {
	self.options = options
	self.total = max(total, len(options))
	self.selection = min(self.selection, max(len(options)-1, 0))
	self.offset = min(self.offset, self.selection)
}

func (self *Dropdown) IsShortOfOptions() bool {
	return self.isOpen && len(self.options) < self.total && self.selection >= len(self.options)-1
}

func (self *Dropdown) SetPlaceholder(placeholder string) {
	self.placeholder = placeholder
}

func (self *Dropdown) Selected() (int, bool) {
	if !self.isOpen || len(self.options) == 0 {
		return 0, false
	}

	return self.selection, true
}

func (self *Dropdown) Apply(keypress key.Key) Outcome {
	if !self.isOpen {
		return Ignored
	}

	isTab := keypress.Code == key.Rune && keypress.Value == '\t' && keypress.Mod == 0
	isShiftTab := keypress.Code == key.Rune && keypress.Value == '\t' && keypress.Mod == key.Shift

	switch {
	case keypress.Code == key.Escape:
		return Dismissed
	case (isTab || keypress.Code == key.Enter && keypress.Mod == 0) && len(self.options) > 0:
		return Chosen
	case isTab, isShiftTab:
		return Swallowed
	case keypress.Code == key.Down && keypress.Mod == 0:
		self.move(1)
		return Moved
	case keypress.Code == key.Up && keypress.Mod == 0:
		self.move(-1)
		return Moved
	default:
		return Ignored
	}
}

func (self *Dropdown) Rows(columns int, budget int) []string {
	if !self.isOpen {
		return nil
	}

	height := min(self.height, max(budget, 1))
	rows := make([]string, 0, height)
	if len(self.options) == 0 {
		rows = append(rows, style.Dim.Over(width.Elide(markerIndent+self.placeholder, columns)))
	}

	visibleRows := self.visibleOptionRows(height)
	offset := min(self.offset, self.selection)
	offset = max(offset, self.selection-visibleRows+1)
	end := min(offset+visibleRows, len(self.options))
	for i := offset; i < end; i++ {
		option := self.elide(self.options[i], columns-width.Of(marker))
		if i == self.selection {
			rows = append(rows, style.ChosenRow.Over(width.Elide(marker+option, columns)))
			continue
		}

		rows = append(rows, style.Dim.Over(width.Elide(markerIndent+option, columns)))
	}

	if note := self.hiddenNote(offset, end); note != "" && len(rows) < height {
		rows = append(rows, style.Dim.Over(width.Elide(markerIndent+note, columns)))
	}

	for len(rows) < height {
		rows = append(rows, "")
	}

	return rows
}

func (self *Dropdown) move(distance int) {
	if len(self.options) == 0 {
		return
	}

	if self.isWhollyShown() {
		self.selection = (self.selection + distance + len(self.options)) % len(self.options)
	} else {
		self.selection = min(max(self.selection+distance, 0), len(self.options)-1)
	}

	visibleRows := self.visibleOptionRows(self.height)
	switch {
	case self.selection < self.offset:
		self.offset = self.selection
	case self.selection >= self.offset+visibleRows:
		self.offset = self.selection - visibleRows + 1
	}
}

func (self *Dropdown) isWhollyShown() bool {
	return self.total <= self.height
}

func (self *Dropdown) visibleOptionRows(height int) int {
	if self.total > height {
		return max(height-1, 1)
	}

	return height
}

func (self *Dropdown) elide(option string, cells int) string {
	if self.elision == ElideStart {
		return width.ElideStart(option, cells)
	}

	return width.Elide(option, cells)
}

func (self *Dropdown) hiddenNote(offset int, end int) string {
	hiddenAbove := offset
	hiddenBelow := self.total - end

	var parts []string
	if hiddenAbove > 0 {
		parts = append(parts, fmt.Sprintf("%d above", hiddenAbove))
	}
	if hiddenBelow > 0 {
		if hiddenAbove > 0 {
			parts = append(parts, fmt.Sprintf("%d below", hiddenBelow))
		} else {
			parts = append(parts, fmt.Sprintf("%d more", hiddenBelow))
		}
	}
	if len(parts) == 0 {
		return ""
	}

	return width.VerticalEllipsis + " " + strings.Join(parts, ", ")
}
