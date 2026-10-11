package output

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/width"
)

type Block interface {
	Rows(columns int) []string
}

type BlockHandle byte

type groupedBlock struct {
	Block

	group  Group
	handle *BlockHandle
}

func (self *Screen) OpenTool(block Block) {
	self.open(block, ToolGroup, nil)
}

type Frame func(rows []string, columns int) []string

type framedBlock struct {
	blocks []Block
	frame  Frame
}

type hiddenNoticeFramer interface {
	FrameHiddenNotice(text string, columns int) string
}

func (self *framedBlock) FrameHiddenNotice(text string, columns int) string {
	for _, block := range self.blocks {
		if framer, ok := block.(hiddenNoticeFramer); ok {
			return framer.FrameHiddenNotice(text, columns)
		}
	}

	return text
}

type LooseBlock interface {
	Block

	IsLoose() bool
}

func isLooseBlock(block Block) bool {
	loose, canBeLoose := block.(LooseBlock)

	return canBeLoose && loose.IsLoose()
}

func (self *framedBlock) Rows(columns int) []string {
	parts := make([]StackPart, 0, len(self.blocks))
	for _, block := range self.blocks {
		parts = append(parts, StackPart{Rows: block.Rows(columns), IsLoose: isLooseBlock(block)})
	}

	return self.frame(Stack(parts), columns)
}

type StackPart struct {
	Rows    []string
	IsLoose bool
}

func Stack(parts []StackPart) []string {
	var rows []string

	isAfterLoosePart := false
	for _, part := range parts {
		if isAfterLoosePart {
			rows = append(rows, "")
		}

		rows = append(rows, part.Rows...)
		isAfterLoosePart = part.IsLoose
	}

	return rows
}

func (self *Screen) Panel(block Block, frame Frame) {
	if self.addToOpenPanel(block) {
		return
	}

	self.open(&framedBlock{blocks: []Block{block}, frame: frame}, PanelGroup, nil)
}

func (self *Screen) SealOpenPanel() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 {
		return false
	}

	if _, isPanel := self.blocks[0].Block.(*framedBlock); !isPanel {
		return false
	}

	self.seal()
	self.changed()

	return true
}

func (self *Screen) addToOpenPanel(block Block) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) == 0 {
		return false
	}

	panel, isPanel := self.blocks[len(self.blocks)-1].Block.(*framedBlock)
	if !isPanel {
		return false
	}

	panel.blocks = append(panel.blocks, block)
	self.changed()

	return true
}

func (self *Screen) OpenPanel(block Block) *BlockHandle {
	self.SealOpenPanel()

	handle := new(BlockHandle)
	self.open(block, PanelGroup, handle)

	return handle
}

func (self *Screen) open(block Block, group Group, handle *BlockHandle) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) == 0 {
		self.seal()
	}

	self.blocks = append(self.blocks, groupedBlock{Block: block, group: group, handle: handle})
	self.changed()
}

func (self *Screen) indexOfBlock(handle *BlockHandle) int {
	if handle == nil {
		return -1
	}

	return slices.IndexFunc(self.blocks, func(block groupedBlock) bool {
		return block.handle == handle
	})
}

func (self *Screen) RefreshBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.indexOfBlock(handle) < 0 {
		return false
	}

	self.live.height = 0
	self.changed()

	return true
}

func (self *Screen) DiscardBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	at := self.indexOfBlock(handle)
	if at < 0 {
		return false
	}

	self.blocks = slices.Delete(self.blocks, at, at+1)
	self.live.height = 0
	self.changed()

	return true
}

func (self *Screen) SealBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.indexOfBlock(handle) < 0 {
		return false
	}

	self.seal()
	self.changed()

	return true
}

func (self *Screen) Seal() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()
	self.changed()
}

func (self *Screen) Refresh() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		self.changed()
	}
}

type priorityRows interface {
	RowsWithPriority(columns int) ([]string, []int)
}

type blockSpan struct {
	start    int
	end      int
	gap      int
	group    Group
	priority []int
	framer   hiddenNoticeFramer
}

func renderGroupedBlocks(blocks []groupedBlock, columns int, grouping Grouping) ([]string, Group, Group) {
	rows, firstGroup, lastGroup, _ := renderGroupedBlocksWithSpans(blocks, columns, grouping)

	return rows, firstGroup, lastGroup
}

func renderGroupedBlocksWithSpans(blocks []groupedBlock, columns int, grouping Grouping) ([]string, Group, Group, []blockSpan) {
	var rows []string
	spans := make([]blockSpan, 0, len(blocks))

	for i, block := range blocks {
		gap := 0
		if i > 0 && !grouping.runsOn(blocks[i-1].group, block.group) {
			rows = append(rows, "")
			gap = 1
		}

		start := len(rows)
		var content []string
		var priority []int
		if important, ok := block.Block.(priorityRows); ok {
			content, priority = important.RowsWithPriority(columns)
		} else {
			content = block.Rows(columns)
		}
		rows = append(rows, content...)
		span := blockSpan{start: start, end: len(rows), gap: gap, group: block.group, priority: priority}
		span.framer, _ = block.Block.(hiddenNoticeFramer)
		spans = append(spans, span)
	}

	return rows, blocks[0].group, blocks[len(blocks)-1].group, spans
}

type textBlock struct {
	text string
}

func (self textBlock) Rows(columns int) []string {
	var rows []string

	for line := range strings.SplitSeq(self.text, "\n") {
		if columns <= 0 {
			rows = append(rows, line)
			continue
		}

		rows = append(rows, width.Wrap(line, columns)...)
	}

	return rows
}
