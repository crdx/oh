package edit

import (
	"unicode"
)

type Buffer struct {
	runes  []rune
	cursor int
}

func (self *Buffer) String() string {
	return string(self.runes)
}

func (self *Buffer) Runes() []rune {
	return self.runes
}

func (self *Buffer) Cursor() int {
	return self.cursor
}

func (self *Buffer) Len() int {
	return len(self.runes)
}

func (self *Buffer) Set(text string) {
	self.runes = []rune(text)
	self.cursor = len(self.runes)
}

func (self *Buffer) Insert(text []rune) {
	rest := append(append([]rune(nil), text...), self.runes[self.cursor:]...)
	self.runes = append(self.runes[:self.cursor], rest...)
	self.cursor += len(text)
}

func (self *Buffer) MoveLeft() {
	if self.cursor > 0 {
		self.cursor--
	}
}

func (self *Buffer) MoveRight() {
	if self.cursor < len(self.runes) {
		self.cursor++
	}
}

func (self *Buffer) MoveHome() {
	self.cursor = self.lineStart()
}

func (self *Buffer) MoveEnd() {
	self.cursor = self.lineEnd()
}

func (self *Buffer) MoveWordLeft() {
	self.cursor = self.wordStart()
}

func (self *Buffer) MoveWordRight() {
	self.cursor = self.wordEnd()
}

func (self *Buffer) MoveUp() bool {
	start := self.lineStart()
	if start == 0 {
		return false
	}

	column := self.cursor - start
	above := self.lineStartAt(start - 1)

	self.cursor = min(above+column, start-1)

	return true
}

func (self *Buffer) MoveDown() bool {
	end := self.lineEnd()
	if end == len(self.runes) {
		return false
	}

	column := self.cursor - self.lineStart()
	below := end + 1

	self.cursor = min(below+column, self.lineEndAt(below))

	return true
}

func (self *Buffer) DeleteBackward() {
	if self.cursor > 0 {
		self.remove(self.cursor-1, self.cursor)
	}
}

func (self *Buffer) DeleteForward() {
	if self.cursor < len(self.runes) {
		self.remove(self.cursor, self.cursor+1)
	}
}

func (self *Buffer) DeleteWordBackward() {
	self.remove(self.wordStart(), self.cursor)
}

func (self *Buffer) DeleteWhitespaceWordBackward() {
	start := self.cursor
	for start > 0 && unicode.IsSpace(self.runes[start-1]) {
		start--
	}
	for start > 0 && !unicode.IsSpace(self.runes[start-1]) {
		start--
	}
	self.remove(start, self.cursor)
}

func (self *Buffer) DeleteToEnd() {
	self.remove(self.cursor, self.lineEnd())
}

func (self *Buffer) remove(start int, end int) {
	if start >= end {
		return
	}

	self.runes = append(self.runes[:start], self.runes[end:]...)
	self.cursor = start
}

func (self *Buffer) lineStart() int {
	return self.lineStartAt(self.cursor)
}

func (self *Buffer) lineStartAt(position int) int {
	for i := position - 1; i >= 0; i-- {
		if self.runes[i] == '\n' {
			return i + 1
		}
	}

	return 0
}

func (self *Buffer) lineEnd() int {
	return self.lineEndAt(self.cursor)
}

func (self *Buffer) lineEndAt(position int) int {
	for i := position; i < len(self.runes); i++ {
		if self.runes[i] == '\n' {
			return i
		}
	}

	return len(self.runes)
}

func (self *Buffer) wordStart() int {
	position := self.cursor

	for position > 0 && !isWord(self.runes[position-1]) {
		position--
	}

	for position > 0 && isWord(self.runes[position-1]) {
		position--
	}

	return position
}

func (self *Buffer) wordEnd() int {
	position := self.cursor

	for position < len(self.runes) && !isWord(self.runes[position]) {
		position++
	}

	for position < len(self.runes) && isWord(self.runes[position]) {
		position++
	}

	return position
}

func isWord(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value)
}
