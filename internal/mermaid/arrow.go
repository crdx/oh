package mermaid

import (
	"container/heap"
	"errors"
	"slices"

	"crdx.org/oh/internal/mermaid/runewidth"
)

type priorityQueueItem struct {
	coord    gridCoord
	priority int
	index    int
}

type priorityQueue []*priorityQueueItem

func (self *priorityQueue) Len() int { return len(*self) }

func (self *priorityQueue) Less(i int, j int) bool {
	return (*self)[i].priority < (*self)[j].priority
}

func (self *priorityQueue) Swap(i int, j int) {
	items := *self
	items[i], items[j] = items[j], items[i]
	items[i].index = i
	items[j].index = j
}

func (self *priorityQueue) Push(value any) {
	item, ok := value.(*priorityQueueItem)
	if !ok {
		panic("priority queue received the wrong item type")
	}
	item.index = len(*self)
	*self = append(*self, item)
}

func (self *priorityQueue) Pop() any {
	old := *self
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*self = old[0 : n-1]
	return item
}

func mustPriorityQueueItem(value any) *priorityQueueItem {
	item, ok := value.(*priorityQueueItem)
	if !ok {
		panic("priority queue returned the wrong item type")
	}
	return item
}

func heuristic(a gridCoord, b gridCoord) int {
	absX := Abs(a.x - b.x)
	absY := Abs(a.y - b.y)
	if absX == 0 || absY == 0 {
		return absX + absY
	} else {
		return absX + absY + 1
	}
}

func (self *graph) getPath(from gridCoord, to gridCoord) ([]gridCoord, error) {
	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &priorityQueueItem{coord: from, priority: 0})

	costSoFar := map[gridCoord]int{from: 0}
	cameFrom := map[gridCoord]*gridCoord{from: nil}

	directions := []gridCoord{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	for pq.Len() > 0 {
		item := mustPriorityQueueItem(heap.Pop(pq))
		current := item.coord

		if current.Equals(to) {
			path := []gridCoord{}
			for c := &current; c != nil; c = cameFrom[*c] {
				path = append([]gridCoord{*c}, path...)
			}
			return path, nil
		}

		for _, dir := range directions {
			next := gridCoord{x: current.x + dir.x, y: current.y + dir.y}
			if !self.isFreeInGrid(next) && !next.Equals(to) {
				continue
			}

			newCost := costSoFar[current] + 1
			if cost, ok := costSoFar[next]; !ok || newCost < cost {
				costSoFar[next] = newCost
				priority := newCost + heuristic(next, to)
				heap.Push(pq, &priorityQueueItem{coord: next, priority: priority})
				cameFrom[next] = &current
			}
		}
	}

	return nil, errors.New("no path found")
}

func (self *graph) isFreeInGrid(c gridCoord) bool {
	if c.x < 0 || c.y < 0 {
		return false
	}
	return self.grid[c] == nil
}

func (self *graph) drawArrow(edge *edge) (*drawing, *drawing, *drawing, *drawing, *drawing) {
	if len(edge.path) == 0 {
		return nil, nil, nil, nil, nil
	}
	dLabel := self.drawArrowLabel(edge)
	dPath, linesDrawn, lineDirs := self.drawPath(edge.path)
	if len(linesDrawn) == 0 {
		return dPath, nil, nil, self.drawCorners(edge.path), dLabel
	}
	dBoxStart := self.drawBoxStart(edge.path, linesDrawn[0])
	dArrowHead := self.drawArrowHead(linesDrawn[len(linesDrawn)-1], lineDirs[len(lineDirs)-1])
	if edge.isBidirectional && len(linesDrawn) > 0 {
		dStartArrowHead := self.drawArrowHead(reverseDrawingLine(linesDrawn[0]), lineDirs[0].getOpposite())
		dArrowHead = self.mergeDrawings(dArrowHead, drawingCoord{0, 0}, dStartArrowHead)
	}
	dCorners := self.drawCorners(edge.path)
	return dPath, dBoxStart, dArrowHead, dCorners, dLabel
}

func reverseDrawingLine(line []drawingCoord) []drawingCoord {
	if len(line) == 0 {
		return line
	}
	reversedLine := make([]drawingCoord, len(line))
	for i, coord := range line {
		reversedLine[len(line)-1-i] = coord
	}
	return reversedLine
}

func mergePath(path []gridCoord) []gridCoord {
	if len(path) <= 2 {
		return path
	}
	indexToRemove := []int{}
	step0 := path[0]
	step1 := path[1]
	for index, step2 := range path[2:] {
		previousDir := determineDirection(genericCoord(step0), genericCoord(step1))
		dir := determineDirection(genericCoord(step1), genericCoord(step2))
		if previousDir == dir {
			indexToRemove = append(indexToRemove, index+1)
		}
		step0 = step1
		step1 = step2
	}
	newPath := []gridCoord{}
	for index, step := range path {
		if !slices.Contains(indexToRemove, index) {
			newPath = append(newPath, step)
		}
	}
	return newPath
}

func (self *graph) drawPath(path []gridCoord) (*drawing, [][]drawingCoord, []direction) {
	sketch := copyCanvas(self.drawing)
	previousCoord := path[0]
	linesDrawn := make([][]drawingCoord, 0)
	lineDirs := make([]direction, 0)
	var previousDrawingCoord drawingCoord
	for _, nextCoord := range path[1:] {
		previousDrawingCoord = self.gridToDrawingCoord(previousCoord)
		nextDrawingCoord := self.gridToDrawingCoord(nextCoord)
		if previousDrawingCoord.Equals(nextDrawingCoord) {
			continue
		}
		dir := determineDirection(genericCoord(previousCoord), genericCoord(nextCoord))
		s := self.drawLine(sketch, previousDrawingCoord, nextDrawingCoord, 1, -1)
		if len(s) == 0 {
			s = append(s, previousDrawingCoord)
		}
		linesDrawn = append(linesDrawn, s)
		lineDirs = append(lineDirs, dir)
		previousCoord = nextCoord
	}
	return sketch, linesDrawn, lineDirs
}

func (self *graph) drawBoxStart(path []gridCoord, firstLine []drawingCoord) *drawing {
	sketch := *copyCanvas(self.drawing)
	from := firstLine[0]
	dir := determineDirection(genericCoord(path[0]), genericCoord(path[1]))

	if self.useAscii {
		return &sketch
	}

	switch dir {
	case Up:
		sketch[from.x][from.y+1] = "┴"
	case Down:
		sketch[from.x][from.y-1] = "┬"
	case Left:
		sketch[from.x+1][from.y] = "┤"
	case Right:
		sketch[from.x-1][from.y] = "├"
	}
	return &sketch
}

func (self *graph) drawArrowHead(line []drawingCoord, fallback direction) *drawing {
	sketch := *copyCanvas(self.drawing)
	if len(line) == 0 {
		return &sketch
	}
	from := line[0]
	lastPosition := line[len(line)-1]
	dir := determineDirection(genericCoord(from), genericCoord(lastPosition))
	if len(line) == 1 || dir == Middle {
		dir = fallback
	}

	var char string
	if !self.useAscii {
		switch dir {
		case Up:
			char = "▲"
		case Down:
			char = "▼"
		case Left:
			char = "◄"
		case Right:
			char = "►"
		case UpperRight:
			char = "◥"
		case UpperLeft:
			char = "◤"
		case LowerRight:
			char = "◢"
		case LowerLeft:
			char = "◣"
		default:
			char = "●"
		}
	} else {
		switch dir {
		case Up:
			char = "^"
		case Down:
			char = "v"
		case Left:
			char = "<"
		case Right:
			char = ">"
		default:
			char = "*"
		}
	}

	sketch[lastPosition.x][lastPosition.y] = char
	return &sketch
}

func (self *graph) drawCorners(path []gridCoord) *drawing {
	sketch := copyCanvas(self.drawing)
	for index, coord := range path {
		if index == 0 || index == len(path)-1 {
			continue
		}
		drawingCoord := self.gridToDrawingCoord(coord)

		previousDir := determineDirection(genericCoord(path[index-1]), genericCoord(coord))
		nextDir := determineDirection(genericCoord(coord), genericCoord(path[index+1]))

		var corner string
		if !self.useAscii {
			switch {
			case (previousDir == Right && nextDir == Down) || (previousDir == Up && nextDir == Left):
				corner = "┐"
			case (previousDir == Right && nextDir == Up) || (previousDir == Down && nextDir == Left):
				corner = "┘"
			case (previousDir == Left && nextDir == Down) || (previousDir == Up && nextDir == Right):
				corner = "┌"
			case (previousDir == Left && nextDir == Up) || (previousDir == Down && nextDir == Right):
				corner = "└"
			default:
				corner = "+"
			}
		} else {
			corner = "+"
		}

		(*sketch)[drawingCoord.x][drawingCoord.y] = corner
	}
	return sketch
}

func (self *graph) drawArrowLabel(edge *edge) *drawing {
	sketch := copyCanvas(self.drawing)
	if edge.text == "" {
		return sketch
	}

	line := self.lineToDrawing(edge.labelLine)
	if edge.isBidirectional {
		line = insetLine(line, 2, 2)
	} else {
		line = insetLine(line, 1, 2)
	}
	sketch.drawTextOnLine(line, edge.text)
	return sketch
}

func insetLine(line []drawingCoord, insetStart int, insetEnd int) []drawingCoord {
	if len(line) < 2 || (insetStart == 0 && insetEnd == 0) {
		return line
	}
	endInset := insetEnd
	if insetEnd > 0 {
		endInset = insetEnd - 1
	}
	dir := determineDirection(genericCoord(line[0]), genericCoord(line[1]))
	start, end := line[0], line[1]
	switch dir {
	case Right:
		start.x += insetStart
		end.x -= endInset
	case Left:
		start.x -= insetStart
		end.x += endInset
	case Down:
		start.y += insetStart
		end.y -= endInset
	case Up:
		start.y -= insetStart
		end.y += endInset
	default:
		return line
	}
	return []drawingCoord{start, end}
}

func (self *drawing) drawTextOnLine(line []drawingCoord, label string) {
	var minX, maxX, minY, maxY int
	if line[0].x > line[1].x {
		minX = line[1].x
		maxX = line[0].x
	} else {
		minX = line[0].x
		maxX = line[1].x
	}
	if line[0].y > line[1].y {
		minY = line[1].y
		maxY = line[0].y
	} else {
		minY = line[0].y
		maxY = line[1].y
	}
	middleX := minX + (maxX-minX)/2
	middleY := minY + (maxY-minY)/2
	startLabelCoord := drawingCoord{x: middleX - runewidth.StringWidth(label)/2, y: middleY}
	self.drawText(startLabelCoord, label)
}
