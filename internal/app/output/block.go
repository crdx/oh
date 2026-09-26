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

func (self *framedBlock) Rows(columns int) []string {
	var rows []string

	for _, block := range self.blocks {
		rows = append(rows, block.Rows(columns)...)
	}

	return self.frame(rows, columns)
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

func renderGroupedBlocks(blocks []groupedBlock, columns int, grouping Grouping) ([]string, Group, Group) {
	var rows []string

	for i, groupedBlock := range blocks {
		if i > 0 && !grouping.runsOn(blocks[i-1].group, groupedBlock.group) {
			rows = append(rows, "")
		}

		rows = append(rows, groupedBlock.Rows(columns)...)
	}

	return rows, blocks[0].group, blocks[len(blocks)-1].group
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
