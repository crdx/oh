package markdown

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/width"
)

type IncrementalRenderer struct {
	stableRows     []width.ScreenRow
	stableSource   string
	previousSource string
	tail           StreamRenderer

	options       Options
	lastCandidate int
	isDisabled    bool
}

func (self *IncrementalRenderer) Render(markdown string, columns int) []string {
	return width.Texts(self.RenderWith(markdown, Options{Columns: columns}))
}

func (self *IncrementalRenderer) RenderWithHyperlinks(markdown string, columns int) []string {
	return width.Texts(self.RenderWith(markdown, Options{Columns: columns, ShouldRenderHyperlinks: true}))
}

func (self *IncrementalRenderer) IsTailMermaid() bool {
	return self.tail.IsTailMermaid()
}

func (self *IncrementalRenderer) EndsWithTable(markdown string) bool {
	if self.isDisabled || self.stableSource == "" || !strings.HasPrefix(markdown, self.stableSource) {
		return EndsWithTable(markdown)
	}

	return EndsWithTable(markdown[len(self.stableSource):])
}

func (self *IncrementalRenderer) Reset() {
	*self = IncrementalRenderer{}
}

func (self *IncrementalRenderer) RenderWith(markdown string, options Options) []width.ScreenRow {
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
		if candidate > self.lastCandidate {
			if remainingRows, isAdvanced := self.advance(markdown, candidate, tailRows); isAdvanced {
				tailRows = remainingRows
			}
		}
	}

	return joinRenderedParts(self.stableRows, tailRows)
}

func (self *IncrementalRenderer) disable(markdown string) []width.ScreenRow {
	self.stableRows = nil
	self.stableSource = ""
	self.tail.Reset()
	self.isDisabled = true

	return self.tail.render(markdown, self.options)
}

func (self *IncrementalRenderer) advance(
	markdown string,
	candidate int,
	tailRows []width.ScreenRow,
) ([]width.ScreenRow, bool) {
	self.lastCandidate = candidate
	settledRows := render(markdown[len(self.stableSource):candidate], self.options, nil)
	var tail StreamRenderer
	remainingRows := tail.render(markdown[candidate:], self.options)
	if tail.hasMermaid || tail.hasLinkReference {
		return nil, false
	}
	if !slices.Equal(tailRows, joinRenderedParts(settledRows, remainingRows)) {
		return nil, false
	}

	self.stableRows = appendRenderedParts(self.stableRows, settledRows)
	self.stableSource = markdown[:candidate]
	self.tail = tail
	return remainingRows, true
}

func appendRenderedParts(stableRows []width.ScreenRow, settledRows []width.ScreenRow) []width.ScreenRow {
	if len(settledRows) == 0 {
		return stableRows
	}
	if len(stableRows) == 0 {
		return slices.Clone(settledRows)
	}

	stableRows = append(stableRows, width.ScreenRow{})
	return append(stableRows, settledRows...)
}

func joinRenderedParts(stableRows []width.ScreenRow, tailRows []width.ScreenRow) []width.ScreenRow {
	if len(stableRows) == 0 {
		return slices.Clone(tailRows)
	}
	if len(tailRows) == 0 {
		return slices.Clone(stableRows)
	}

	rows := make([]width.ScreenRow, 0, len(stableRows)+1+len(tailRows))
	rows = append(rows, stableRows...)
	rows = append(rows, width.ScreenRow{})
	return append(rows, tailRows...)
}
