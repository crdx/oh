package markdown

import (
	"slices"
	"strings"

	"crdx.org/io/internal/app/link"
)

type IncrementalRenderer struct {
	stableRows     []string
	stableSource   string
	previousSource string
	tail           StreamRenderer

	options       Options
	lastCandidate int
	isDisabled    bool
}

func (self *IncrementalRenderer) Render(markdown string, columns int) []string {
	return self.RenderWith(markdown, Options{Columns: columns})
}

func (self *IncrementalRenderer) RenderWithHyperlinks(markdown string, columns int) []string {
	return self.RenderWith(markdown, Options{Columns: columns, ShouldRenderHyperlinks: true})
}

func (self *IncrementalRenderer) RenderWithHyperlinksUnder(
	markdown string,
	columns int,
	linkRoot link.Roots,
) []string {
	return self.RenderWith(markdown, Options{
		Columns:                columns,
		ShouldRenderHyperlinks: true,
		LinkRoot:               linkRoot,
	})
}

func (self *IncrementalRenderer) IsTailMermaid() bool {
	return self.tail.IsTailMermaid()
}

func (self *IncrementalRenderer) Reset() {
	*self = IncrementalRenderer{}
}

func (self *IncrementalRenderer) RenderWith(markdown string, options Options) []string {
	if options != self.options || !strings.HasPrefix(markdown, self.previousSource) {
		self.Reset()
		self.options = options
	}
	self.previousSource = markdown
	if self.isDisabled {
		return self.tail.render(markdown, options)
	}

	tailRows := self.tail.render(markdown[len(self.stableSource):], options)
	if self.tail.hasMermaid || self.tail.hasLinkReference {
		return self.disable(markdown)
	}
	if self.tail.hasStableCandidateStart && self.tail.stableCandidateStart > 0 {
		candidate := len(self.stableSource) + self.tail.stableCandidateStart
		if candidate > self.lastCandidate && self.advance(markdown, candidate) {
			tailRows = self.tail.render(markdown[len(self.stableSource):], options)
		}
	}

	return joinRenderedParts(self.stableRows, tailRows)
}

func (self *IncrementalRenderer) disable(markdown string) []string {
	self.stableRows = nil
	self.stableSource = ""
	self.tail.Reset()
	self.isDisabled = true

	return self.tail.render(markdown, self.options)
}

func (self *IncrementalRenderer) advance(markdown string, candidate int) bool {
	self.lastCandidate = candidate
	stableRows := render(markdown[:candidate], self.options, nil)
	var tail StreamRenderer
	tailRows := tail.render(markdown[candidate:], self.options)
	if tail.hasMermaid || tail.hasLinkReference {
		return false
	}
	fullRows := render(markdown, self.options, nil)
	if !slices.Equal(fullRows, joinRenderedParts(stableRows, tailRows)) {
		return false
	}

	self.stableRows = stableRows
	self.stableSource = markdown[:candidate]
	self.tail.Reset()
	return true
}

func joinRenderedParts(stableRows []string, tailRows []string) []string {
	if len(stableRows) == 0 {
		return slices.Clone(tailRows)
	}
	if len(tailRows) == 0 {
		return slices.Clone(stableRows)
	}

	rows := make([]string, 0, len(stableRows)+1+len(tailRows))
	rows = append(rows, stableRows...)
	rows = append(rows, "")
	return append(rows, tailRows...)
}
