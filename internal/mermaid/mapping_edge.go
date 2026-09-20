package mermaid

import "crdx.org/oh/internal/mermaid/runewidth"

type edge struct {
	from            *node
	to              *node
	text            string
	isBidirectional bool
	path            []gridCoord
	labelLine       []gridCoord
	startDir        direction
	endDir          direction
}

func (self *graph) determinePath(edge *edge) {
	key := newEdgePair(edge.from.index, edge.to.index)
	duplicateIndex := self.edgeCounts[key]

	if startDir, endDir, ok := self.parallelDirections(edge, duplicateIndex); ok {
		from := edge.from.gridCoord.Direction(startDir)
		to := edge.to.gridCoord.Direction(endDir)
		if path, err := self.getPath(from, to); err == nil {
			edge.startDir = startDir
			edge.endDir = endDir
			edge.path = mergePath(path)
			self.edgeCounts[key]++
			return
		}
	}

	preferredDir, preferredOppositeDir, alternativeDir, alternativeOppositeDir := self.determineStartAndEndDir(edge)
	preferredFrom := edge.from.gridCoord.Direction(preferredDir)
	preferredTo := edge.to.gridCoord.Direction(preferredOppositeDir)
	preferredPath, preferredError := self.getPath(preferredFrom, preferredTo)
	if preferredError == nil {
		preferredPath = mergePath(preferredPath)
	}

	alternativeFrom := edge.from.gridCoord.Direction(alternativeDir)
	alternativeTo := edge.to.gridCoord.Direction(alternativeOppositeDir)
	alternativePath, alternativeError := self.getPath(alternativeFrom, alternativeTo)
	if alternativeError == nil {
		alternativePath = mergePath(alternativePath)
	}

	switch {
	case preferredError == nil && (alternativeError != nil || len(preferredPath) <= len(alternativePath)):
		edge.startDir = preferredDir
		edge.endDir = preferredOppositeDir
		edge.path = preferredPath
	case alternativeError == nil:
		edge.startDir = alternativeDir
		edge.endDir = alternativeOppositeDir
		edge.path = alternativePath
	default:
		return
	}
	self.edgeCounts[key]++
}

func (self *graph) parallelDirections(e *edge, duplicateIndex int) (direction, direction, bool) {
	if duplicateIndex == 0 {
		return Middle, Middle, false
	}

	dir := determineDirection(genericCoord(*e.from.gridCoord), genericCoord(*e.to.gridCoord))
	switch {
	case self.graphDirection == "LR" && (dir == Right || dir == Left):
		options := [][2]direction{{Down, Down}, {Up, Up}}
		if duplicateIndex-1 < len(options) {
			return options[duplicateIndex-1][0], options[duplicateIndex-1][1], true
		}
	case self.graphDirection == "TD" && (dir == Down || dir == Up):
		options := [][2]direction{{Right, Right}, {Left, Left}}
		if duplicateIndex-1 < len(options) {
			return options[duplicateIndex-1][0], options[duplicateIndex-1][1], true
		}
	}

	return Middle, Middle, false
}

func (self *graph) determineLabelLine(edge *edge) {
	lenLabel := runewidth.StringWidth(edge.text)
	if lenLabel == 0 {
		return
	}
	previousStep := edge.path[0]
	var largestLine []gridCoord
	var largestLineSize int
	var fallbackLine []gridCoord
	var fallbackLineSize int
	for _, step := range edge.path[1:] {
		line := []gridCoord{previousStep, step}
		previousStep = step
		lineWidth := self.calculateLineWidth(line)
		if self.isNodeColumn(labelMiddleX(line)) {
			if lineWidth > fallbackLineSize {
				fallbackLineSize = lineWidth
				fallbackLine = line
			}
			continue
		}
		if lineWidth >= lenLabel {
			largestLine = line
			break
		}
		if lineWidth > largestLineSize {
			largestLineSize = lineWidth
			largestLine = line
		}
	}
	if largestLine == nil {
		largestLine = fallbackLine
	}
	if largestLine == nil {
		largestLine = []gridCoord{edge.path[0], edge.path[1]}
	}

	middleX := labelMiddleX(largestLine)
	labelPadding := 3
	if edge.isBidirectional {
		labelPadding = 4
	}
	self.columnWidth[middleX] = Max(self.columnWidth[middleX], lenLabel+labelPadding)
	edge.labelLine = largestLine
}

func labelMiddleX(line []gridCoord) int {
	minX, maxX := line[0].x, line[1].x
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	return minX + (maxX-minX)/2
}

func (self *graph) isNodeColumn(x int) bool {
	for _, n := range self.nodes {
		if n.gridCoord == nil {
			continue
		}
		if x >= n.gridCoord.x && x <= n.gridCoord.x+2 {
			return true
		}
	}
	return false
}

func (self *graph) calculateLineWidth(line []gridCoord) int {
	totalSize := 0
	for _, c := range line {
		totalSize += self.columnWidth[c.x]
	}
	return totalSize
}
