package link

import (
	"os"
	"strings"
	"sync"
)

const pathMarkers = "./<"

func pathMatches(text string) [][]int {
	var matches [][]int

	for begin := 0; begin < len(text); {
		for begin < len(text) && isSpace(text[begin]) {
			begin++
		}
		end := begin
		for end < len(text) && !isSpace(text[end]) {
			end++
		}

		token := text[begin:end]
		if strings.ContainsAny(token, pathMarkers) {
			for _, match := range pathPattern.FindAllStringSubmatchIndex(token, -1) {
				for i := range match {
					if match[i] >= 0 {
						match[i] += begin
					}
				}
				matches = append(matches, match)
			}
		}

		begin = end
	}

	return matches
}

func isSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

type memory struct {
	mutex      sync.Mutex
	depth      int
	existences map[string]bool
}

var existenceMemory memory

func Remembering(draw func()) {
	existenceMemory.begin()
	defer existenceMemory.end()

	draw()
}

func (self *memory) begin() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.depth == 0 {
		self.existences = map[string]bool{}
	}
	self.depth++
}

func (self *memory) end() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.depth--
	if self.depth == 0 {
		self.existences = nil
	}
}

func (self *memory) exists(path string) bool {
	self.mutex.Lock()
	isPresent, isKnown := self.existences[path]
	isRemembering := self.existences != nil
	self.mutex.Unlock()

	if isKnown {
		return isPresent
	}

	_, err := os.Stat(path)
	isPresent = err == nil

	if isRemembering {
		self.mutex.Lock()
		if self.existences != nil {
			self.existences[path] = isPresent
		}
		self.mutex.Unlock()
	}

	return isPresent
}
