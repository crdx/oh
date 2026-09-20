package mermaid

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/internal/mermaid/color"
	"crdx.org/oh/internal/mermaid/runewidth"
)

var junctionChars = []string{
	"─",
	"│",
	"┌",
	"┐",
	"└",
	"┘",
	"├",
	"┤",
	"┬",
	"┴",
	"┼",
	"╴",
	"╵",
	"╶",
	"╷",
}

type drawing [][]string

type styleClass struct {
	name   string
	styles map[string]string
}

func (self *graph) drawNode(n *node) {
	m := self.mergeDrawings(self.drawing, *n.drawingCoord, n.drawing)
	self.drawing = m
}

func (self *graph) drawEdge(e *edge) (*drawing, *drawing, *drawing, *drawing, *drawing) {
	return self.drawArrow(e)
}

func (self *drawing) drawText(start drawingCoord, text string) {
	cells := runewidth.Cells(text)
	self.increaseSize(start.x+len(cells), start.y)
	for i, cell := range cells {
		(*self)[start.x+i][start.y] = cell
	}
}

func (self *graph) drawLine(target *drawing, from drawingCoord, to drawingCoord, offsetFrom int, offsetTo int) []drawingCoord {
	direction := determineDirection(genericCoord(from), genericCoord(to))
	vertical, horizontal, risingDiagonal, fallingDiagonal := "│", "─", "╱", "╲"
	if self.useAscii {
		vertical, horizontal, risingDiagonal, fallingDiagonal = "|", "-", "/", "\\"
	}

	drawnCoords := make([]drawingCoord, 0)
	switch direction {
	case Up:
		for y := from.y - offsetFrom; y >= to.y-offsetTo; y-- {
			drawnCoords = append(drawnCoords, drawingCoord{from.x, y})
			(*target)[from.x][y] = vertical
		}
	case Down:
		for y := from.y + offsetFrom; y <= to.y+offsetTo; y++ {
			drawnCoords = append(drawnCoords, drawingCoord{from.x, y})
			(*target)[from.x][y] = vertical
		}
	case Left:
		for x := from.x - offsetFrom; x >= to.x-offsetTo; x-- {
			drawnCoords = append(drawnCoords, drawingCoord{x, from.y})
			(*target)[x][from.y] = horizontal
		}
	case Right:
		for x := from.x + offsetFrom; x <= to.x+offsetTo; x++ {
			drawnCoords = append(drawnCoords, drawingCoord{x, from.y})
			(*target)[x][from.y] = horizontal
		}
	case UpperLeft:
		for x, y := from.x, from.y-offsetFrom; x >= to.x-offsetTo && y >= to.y-offsetTo; x, y = x-1, y-1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*target)[x][y] = fallingDiagonal
		}
	case UpperRight:
		for x, y := from.x, from.y-offsetFrom; x <= to.x+offsetTo && y >= to.y-offsetTo; x, y = x+1, y-1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*target)[x][y] = risingDiagonal
		}
	case LowerLeft:
		for x, y := from.x, from.y+offsetFrom; x >= to.x-offsetTo && y <= to.y+offsetTo; x, y = x-1, y+1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*target)[x][y] = risingDiagonal
		}
	case LowerRight:
		for x, y := from.x, from.y+offsetFrom; x <= to.x+offsetTo && y <= to.y+offsetTo; x, y = x+1, y+1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*target)[x][y] = fallingDiagonal
		}
	}
	return drawnCoords
}

func drawMap(properties *graphProperties) (string, error) {
	graph := mkGraph(properties.data, properties.nodeSpecs)
	graph.setStyleClasses(properties)
	graph.paddingX = properties.paddingX
	graph.paddingY = properties.paddingY
	graph.useAscii = properties.useAscii
	graph.setSubgraphs(properties.subgraphs)
	if err := graph.createMapping(); err != nil {
		return "", err
	}
	return drawingToString(graph.draw()), nil
}

func drawBox(node *node, graph *graph) *drawing {
	width := 0
	for i := range 2 {
		width += graph.columnWidth[node.gridCoord.x+i]
	}
	height := 0
	for i := range 2 {
		height += graph.rowHeight[node.gridCoord.y+i]
	}

	from := drawingCoord{0, 0}
	to := drawingCoord{width, height}
	boxDrawing := *mkDrawing(Max(from.x, to.x), Max(from.y, to.y))
	drawRectangleBorder(&boxDrawing, from, to, graph.useAscii)
	innerTop := from.y + 1
	innerHeight := height - 1
	contentTop := innerTop + (innerHeight-node.label.contentHeight())/2
	for lineIndex, line := range node.label.lines {
		textY := contentTop + lineIndex*(graphLabelLineGap+1)
		textWidth := runewidth.StringWidth(line)
		textX := from.x + width/2 - CeilDiv(textWidth, 2) + 1
		for _, cell := range runewidth.Cells(line) {
			if cell != "" {
				cell = wrapTextInColor(cell, node.styleClass.styles["color"], graph.styleType)
			}
			boxDrawing[textX][textY] = cell
			textX++
		}
	}

	return &boxDrawing
}

func drawSubgraph(sg *subgraph, graph graph) *drawing {
	width := sg.maxX - sg.minX
	height := sg.maxY - sg.minY

	if width <= 0 || height <= 0 {
		return mkDrawing(0, 0)
	}

	from := drawingCoord{0, 0}
	to := drawingCoord{width, height}
	subgraphDrawing := *mkDrawing(width, height)

	drawRectangleBorder(&subgraphDrawing, from, to, graph.useAscii)
	return &subgraphDrawing
}

func drawRectangleBorder(target *drawing, from drawingCoord, to drawingCoord, shouldUseAscii bool) {
	horizontal, vertical := "─", "│"
	topLeft, topRight, bottomLeft, bottomRight := "┌", "┐", "└", "┘"
	if shouldUseAscii {
		horizontal, vertical = "-", "|"
		topLeft, topRight, bottomLeft, bottomRight = "+", "+", "+", "+"
	}

	for x := from.x + 1; x < to.x; x++ {
		(*target)[x][from.y] = horizontal
		(*target)[x][to.y] = horizontal
	}
	for y := from.y + 1; y < to.y; y++ {
		(*target)[from.x][y] = vertical
		(*target)[to.x][y] = vertical
	}
	(*target)[from.x][from.y] = topLeft
	(*target)[to.x][from.y] = topRight
	(*target)[from.x][to.y] = bottomLeft
	(*target)[to.x][to.y] = bottomRight
}

func drawSubgraphLabel(sg *subgraph) (*drawing, drawingCoord) {
	width := sg.maxX - sg.minX
	height := sg.maxY - sg.minY

	if width <= 0 || height <= 0 {
		return mkDrawing(0, 0), drawingCoord{0, 0}
	}

	from := drawingCoord{0, 0}
	to := drawingCoord{width, height}
	labelDrawing := *mkDrawing(width, height)

	for lineIndex, line := range sg.label.lines {
		labelY := from.y + 1 + lineIndex*(graphLabelLineGap+1)
		labelX := max(from.x+width/2-runewidth.StringWidth(line)/2, from.x+1)
		for _, cell := range runewidth.Cells(line) {
			if labelX < to.x {
				labelDrawing[labelX][labelY] = cell
			}
			labelX++
		}
	}

	offset := drawingCoord{sg.minX, sg.minY}
	return &labelDrawing, offset
}

func wrapTextInColor(text string, colorName string, styleType string) string {
	if colorName == "" {
		return text
	}
	switch styleType {
	case "html":
		return fmt.Sprintf("<span style='color: %s'>%s</span>", colorName, text)
	case "cli":
		cliColor := color.HEX(colorName)
		return cliColor.Sprint(text)
	default:
		return text
	}
}

func (self *drawing) increaseSize(x int, y int) {
	currentSizeX, currentSizeY := getDrawingSize(self)
	drawingWithNewSize := mkDrawing(Max(x, currentSizeX), Max(y, currentSizeY))
	for x := range len(*drawingWithNewSize) {
		for y := range len((*drawingWithNewSize)[0]) {
			if x < len(*self) && y < len((*self)[0]) {
				(*drawingWithNewSize)[x][y] = (*self)[x][y]
			}
		}
	}
	*self = *drawingWithNewSize
}

func (self *graph) setDrawingSizeToGridConstraints() {
	maxX := 0
	maxY := 0
	for _, w := range self.columnWidth {
		maxX += w
	}
	for _, h := range self.rowHeight {
		maxY += h
	}
	self.drawing.increaseSize(maxX-1, maxY-1)
}

func mergeJunctions(c1 string, c2 string) string {
	junctionMap := map[string]map[string]string{
		"─": {"│": "┼", "┌": "┬", "┐": "┬", "└": "┴", "┘": "┴", "├": "┼", "┤": "┼", "┬": "┬", "┴": "┴"},
		"│": {"─": "┼", "┌": "├", "┐": "┤", "└": "├", "┘": "┤", "├": "├", "┤": "┤", "┬": "┼", "┴": "┼"},
		"┌": {"─": "┬", "│": "├", "┐": "┬", "└": "├", "┘": "┼", "├": "├", "┤": "┼", "┬": "┬", "┴": "┼"},
		"┐": {"─": "┬", "│": "┤", "┌": "┬", "└": "┼", "┘": "┤", "├": "┼", "┤": "┤", "┬": "┬", "┴": "┼"},
		"└": {"─": "┴", "│": "├", "┌": "├", "┐": "┼", "┘": "┴", "├": "├", "┤": "┼", "┬": "┼", "┴": "┴"},
		"┘": {"─": "┴", "│": "┤", "┌": "┼", "┐": "┤", "└": "┴", "├": "┼", "┤": "┤", "┬": "┼", "┴": "┴"},
		"├": {"─": "┼", "│": "├", "┌": "├", "┐": "┼", "└": "├", "┘": "┼", "┤": "┼", "┬": "┼", "┴": "┼"},
		"┤": {"─": "┼", "│": "┤", "┌": "┼", "┐": "┤", "└": "┼", "┘": "┤", "├": "┼", "┬": "┼", "┴": "┼"},
		"┬": {"─": "┬", "│": "┼", "┌": "┬", "┐": "┬", "└": "┼", "┘": "┼", "├": "┼", "┤": "┼", "┴": "┼"},
		"┴": {"─": "┴", "│": "┼", "┌": "┼", "┐": "┼", "└": "┴", "┘": "┴", "├": "┼", "┤": "┼", "┬": "┼"},
	}

	if mergedJunction, ok := junctionMap[c1][c2]; ok {
		return mergedJunction
	}

	return c1
}

func (self *graph) mergeDrawings(baseDrawing *drawing, mergeCoord drawingCoord, drawings ...*drawing) *drawing {
	maxX, maxY := getDrawingSize(baseDrawing)
	for _, d := range drawings {
		if d == nil {
			continue
		}
		dX, dY := getDrawingSize(d)
		maxX = Max(maxX, dX+mergeCoord.x)
		maxY = Max(maxY, dY+mergeCoord.y)
	}

	mergedDrawing := mkDrawing(maxX, maxY)

	for x := 0; x <= maxX; x++ {
		for y := 0; y <= maxY; y++ {
			if x < len(*baseDrawing) && y < len((*baseDrawing)[0]) {
				(*mergedDrawing)[x][y] = (*baseDrawing)[x][y]
			}
		}
	}

	for _, layer := range drawings {
		if layer == nil {
			continue
		}
		for x := range len(*layer) {
			for y := range len((*layer)[0]) {
				cell := (*layer)[x][y]
				if cell != " " {
					currentChar := (*mergedDrawing)[x+mergeCoord.x][y+mergeCoord.y]
					if !self.useAscii && isJunctionChar(cell) && isJunctionChar(currentChar) {
						(*mergedDrawing)[x+mergeCoord.x][y+mergeCoord.y] = mergeJunctions(currentChar, cell)
					} else {
						(*mergedDrawing)[x+mergeCoord.x][y+mergeCoord.y] = cell
					}
				}
			}
		}
	}

	return mergedDrawing
}

func isJunctionChar(c string) bool {
	return slices.Contains(junctionChars, c)
}

func drawingToString(d *drawing) string {
	maxX, maxY := getDrawingSize(d)
	dBuilder := strings.Builder{}
	for y := 0; y <= maxY; y++ {
		for x := 0; x <= maxX; x++ {
			dBuilder.WriteString((*d)[x][y])
		}
		if y != maxY {
			dBuilder.WriteString("\n")
		}
	}
	return dBuilder.String()
}

func mkDrawing(x int, y int) *drawing {
	result := make(drawing, x+1)
	for i := 0; i <= x; i++ {
		result[i] = make([]string, y+1)
		for j := 0; j <= y; j++ {
			result[i][j] = " "
		}
	}
	return &result
}

func copyCanvas(toBeCopied *drawing) *drawing {
	x, y := getDrawingSize(toBeCopied)
	return mkDrawing(x, y)
}

func getDrawingSize(d *drawing) (int, int) {
	return len(*d) - 1, len((*d)[0]) - 1
}

func determineDirection(from genericCoord, to genericCoord) direction {
	switch {
	case from.x == to.x:
		if from.y < to.y {
			return Down
		}
		return Up
	case from.y == to.y:
		if from.x < to.x {
			return Right
		}
		return Left
	case from.x < to.x:
		if from.y < to.y {
			return LowerRight
		}
		return UpperRight
	default:
		if from.y < to.y {
			return LowerLeft
		}
		return UpperLeft
	}
}
