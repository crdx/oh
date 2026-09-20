package mermaid

import (
	"errors"
	"slices"

	"crdx.org/oh/internal/mermaid/orderedmap"
)

type genericCoord struct {
	x int
	y int
}

type (
	gridCoord    genericCoord
	drawingCoord genericCoord
)

func (self gridCoord) Equals(other gridCoord) bool {
	return self.x == other.x && self.y == other.y
}

func (self drawingCoord) Equals(other drawingCoord) bool {
	return self.x == other.x && self.y == other.y
}

func (self *graph) lineToDrawing(line []gridCoord) []drawingCoord {
	dc := []drawingCoord{}
	for _, c := range line {
		dc = append(dc, self.gridToDrawingCoord(c))
	}
	return dc
}

type graph struct {
	nodes            []*node
	edges            []*edge
	drawing          *drawing
	grid             map[gridCoord]*node
	edgeCounts       map[edgePair]int
	columnWidth      map[int]int
	rowHeight        map[int]int
	styleClasses     map[string]styleClass
	styleType        string
	boxBorderPadding int
	graphDirection   string
	paddingX         int
	paddingY         int
	subgraphs        []*subgraph
	offsetX          int
	offsetY          int
	useAscii         bool
}

type edgePair struct {
	from int
	to   int
}

func newEdgePair(from int, to int) edgePair {
	if from < to {
		return edgePair{from: from, to: to}
	}
	return edgePair{from: to, to: from}
}

type subgraph struct {
	name     string
	label    graphLabel
	nodes    []*node
	parent   *subgraph
	children []*subgraph
	minX     int
	minY     int
	maxX     int
	maxY     int
}

func mkGraph(data *orderedmap.OrderedMap[string, []textEdge], nodeSpecs map[string]graphNodeSpec) graph {
	builtGraph := graph{drawing: mkDrawing(0, 0)}
	builtGraph.grid = make(map[gridCoord]*node)
	builtGraph.edgeCounts = make(map[edgePair]int)
	builtGraph.columnWidth = make(map[int]int)
	builtGraph.rowHeight = make(map[int]int)
	builtGraph.styleClasses = make(map[string]styleClass)
	index := 0
	for el := data.Front(); el != nil; el = el.Next() {
		nodeName := el.Key
		children := el.Value
		spec := nodeSpecs[nodeName]
		parentNode, err := builtGraph.getNode(nodeName)
		if err != nil {
			parentNode = &node{name: nodeName, label: spec.label, index: index, styleClassName: spec.styleClass}
			builtGraph.appendNode(parentNode)
			index += 1
		}
		for _, textEdge := range children {
			childSpec := nodeSpecs[textEdge.child.name]
			childNode, err := builtGraph.getNode(textEdge.child.name)
			if err != nil {
				childNode = &node{name: textEdge.child.name, label: childSpec.label, index: index, styleClassName: childSpec.styleClass}
				builtGraph.appendNode(childNode)
				index += 1
			}
			createdEdge := edge{
				from:            parentNode,
				to:              childNode,
				text:            textEdge.label,
				isBidirectional: textEdge.isBidirectional,
			}
			builtGraph.edges = append(builtGraph.edges, &createdEdge)
		}
	}
	return builtGraph
}

func (self *graph) setStyleClasses(properties *graphProperties) {
	self.styleClasses = *properties.styleClasses
	self.styleType = properties.styleType
	self.boxBorderPadding = properties.boxBorderPadding
	self.graphDirection = properties.graphDirection
	self.paddingX = properties.paddingX
	self.paddingY = properties.paddingY
	for _, n := range self.nodes {
		if n.styleClassName != "" {
			n.styleClass = self.styleClasses[n.styleClassName]
		}
	}
}

func (self *graph) setSubgraphs(textSubgraphs []*textSubgraph) {
	self.subgraphs = []*subgraph{}

	for _, tsg := range textSubgraphs {
		sg := &subgraph{
			name:     tsg.name,
			label:    tsg.label,
			nodes:    []*node{},
			children: []*subgraph{},
		}

		for _, nodeName := range tsg.nodes {
			node, err := self.getNode(nodeName)
			if err == nil {
				sg.nodes = append(sg.nodes, node)
			}
		}

		self.subgraphs = append(self.subgraphs, sg)
	}

	for i, tsg := range textSubgraphs {
		sg := self.subgraphs[i]

		if tsg.parent != nil {
			for j, parentTsg := range textSubgraphs {
				if parentTsg == tsg.parent {
					sg.parent = self.subgraphs[j]
					break
				}
			}
		}

		for _, childTsg := range tsg.children {
			for j, checkTsg := range textSubgraphs {
				if checkTsg == childTsg {
					sg.children = append(sg.children, self.subgraphs[j])
					break
				}
			}
		}
	}
}

func (self *graph) createMapping() error {
	highestPositionPerLevel := map[int]int{}

	nodesFound := make(map[string]bool)
	rootNodes := []*node{}
	for _, n := range self.nodes {
		if _, ok := nodesFound[n.name]; !ok {
			rootNodes = append(rootNodes, n)
		}
		nodesFound[n.name] = true
		for _, child := range self.getChildren(n) {
			nodesFound[child.name] = true
		}
	}

	hasExternalRoots := false
	hasSubgraphRootsWithEdges := false
	for _, n := range rootNodes {
		if self.isNodeInAnySubgraph(n) {
			if len(self.getChildren(n)) > 0 {
				hasSubgraphRootsWithEdges = true
			}
		} else {
			hasExternalRoots = true
		}
	}

	shouldSeparate := self.graphDirection == "LR" && hasExternalRoots && hasSubgraphRootsWithEdges

	externalRootNodes := []*node{}
	subgraphRootNodes := []*node{}
	if shouldSeparate {
		for _, n := range rootNodes {
			if self.isNodeInAnySubgraph(n) {
				subgraphRootNodes = append(subgraphRootNodes, n)
			} else {
				externalRootNodes = append(externalRootNodes, n)
			}
		}
	} else {
		externalRootNodes = rootNodes
	}

	for _, node := range externalRootNodes {
		var mappingCoord *gridCoord
		if self.graphDirection == "LR" {
			mappingCoord = self.reserveSpotInGrid(self.nodes[node.index], &gridCoord{x: 0, y: highestPositionPerLevel[0]})
		} else {
			mappingCoord = self.reserveSpotInGrid(self.nodes[node.index], &gridCoord{x: highestPositionPerLevel[0], y: 0})
		}
		self.nodes[node.index].gridCoord = mappingCoord
		highestPositionPerLevel[0] += 4
	}

	if shouldSeparate && len(subgraphRootNodes) > 0 {
		subgraphLevel := 4
		for _, n := range subgraphRootNodes {
			mappingCoord := self.reserveSpotInGrid(self.nodes[n.index], &gridCoord{x: subgraphLevel, y: highestPositionPerLevel[subgraphLevel]})
			self.nodes[n.index].gridCoord = mappingCoord
			highestPositionPerLevel[subgraphLevel] += 4
		}
	}

	for _, node := range self.nodes {
		var childLevel int
		if self.graphDirection == "LR" {
			childLevel = node.gridCoord.x + 4
		} else {
			childLevel = node.gridCoord.y + 4
		}
		highestPosition := highestPositionPerLevel[childLevel]
		for _, child := range self.getChildren(node) {
			if child.gridCoord != nil {
				continue
			}

			var mappingCoord *gridCoord
			if self.graphDirection == "LR" {
				mappingCoord = self.reserveSpotInGrid(self.nodes[child.index], &gridCoord{x: childLevel, y: highestPosition})
			} else {
				mappingCoord = self.reserveSpotInGrid(self.nodes[child.index], &gridCoord{x: highestPosition, y: childLevel})
			}
			self.nodes[child.index].gridCoord = mappingCoord
			highestPositionPerLevel[childLevel] = highestPosition + 4
		}
	}

	for _, n := range self.nodes {
		self.setColumnWidth(n)
	}

	for _, e := range self.edges {
		self.determinePath(e)
		self.increaseGridSizeForPath(e.path)
		self.determineLabelLine(e)
	}

	for _, n := range self.nodes {
		dc := self.gridToDrawingCoord(*n.gridCoord)
		self.nodes[n.index].setCoord(&dc)
		self.nodes[n.index].setDrawing(self)
	}

	self.calculateSubgraphBoundingBoxes()

	self.offsetDrawingForSubgraphs()
	if err := self.validateDrawingSize(); err != nil {
		return err
	}
	self.setDrawingSizeToGridConstraints()
	return nil
}

func (self *graph) validateDrawingSize() error {
	columns := self.offsetX
	rows := self.offsetY
	for _, width := range self.columnWidth {
		columns += width
	}
	for _, height := range self.rowHeight {
		rows += height
	}
	for _, subgraph := range self.subgraphs {
		columns = max(columns, subgraph.maxX+1)
		rows = max(rows, subgraph.maxY+1)
	}
	return validateCanvasLimits(columns, rows)
}

func (self *graph) calculateSubgraphBoundingBoxes() {
	for _, sg := range self.subgraphs {
		self.calculateSubgraphBoundingBox(sg)
	}

	self.ensureSubgraphSpacing()
}

func (self *graph) isNodeInAnySubgraph(n *node) bool {
	for _, sg := range self.subgraphs {
		if slices.Contains(sg.nodes, n) {
			return true
		}
	}
	return false
}

func (self *graph) getNodeSubgraph(n *node) *subgraph {
	for _, sg := range self.subgraphs {
		if slices.Contains(sg.nodes, n) {
			return sg
		}
	}
	return nil
}

func (self *graph) hasIncomingEdgeFromOutsideSubgraph(node *node) bool {
	nodeSubgraph := self.getNodeSubgraph(node)
	if nodeSubgraph == nil {
		return false
	}

	hasExternalEdge := false
	for _, edge := range self.edges {
		if edge.to == node {
			sourceSubgraph := self.getNodeSubgraph(edge.from)
			if sourceSubgraph != nodeSubgraph {
				hasExternalEdge = true
				break
			}
		}
	}

	if !hasExternalEdge {
		return false
	}

	for _, otherNode := range nodeSubgraph.nodes {
		if otherNode == node || otherNode.gridCoord == nil {
			continue
		}
		hasOtherExternal := false
		for _, edge := range self.edges {
			if edge.to == otherNode {
				sourceSubgraph := self.getNodeSubgraph(edge.from)
				if sourceSubgraph != nodeSubgraph {
					hasOtherExternal = true
					break
				}
			}
		}
		if hasOtherExternal && otherNode.gridCoord.y < node.gridCoord.y {
			return false
		}
	}

	return true
}

func (self *graph) ensureSubgraphSpacing() {
	const minSpacing = 1

	rootSubgraphs := []*subgraph{}
	for _, sg := range self.subgraphs {
		if sg.parent == nil && len(sg.nodes) > 0 {
			rootSubgraphs = append(rootSubgraphs, sg)
		}
	}

	for i := range rootSubgraphs {
		for j := i + 1; j < len(rootSubgraphs); j++ {
			sg1 := rootSubgraphs[i]
			sg2 := rootSubgraphs[j]

			if sg1.minX < sg2.maxX && sg1.maxX > sg2.minX {
				if sg1.maxY >= sg2.minY-minSpacing && sg1.minY < sg2.minY {
					newMinY := sg1.maxY + minSpacing + 1
					sg2.minY = newMinY
				} else if sg2.maxY >= sg1.minY-minSpacing && sg2.minY < sg1.minY {
					newMinY := sg2.maxY + minSpacing + 1
					sg1.minY = newMinY
				}
			}

			if sg1.minY < sg2.maxY && sg1.maxY > sg2.minY {
				if sg1.maxX >= sg2.minX-minSpacing && sg1.minX < sg2.minX {
					newMinX := sg1.maxX + minSpacing + 1
					sg2.minX = newMinX
				} else if sg2.maxX >= sg1.minX-minSpacing && sg2.minX < sg1.minX {
					newMinX := sg2.maxX + minSpacing + 1
					sg1.minX = newMinX
				}
			}
		}
	}
}

func (self *graph) calculateSubgraphBoundingBox(sg *subgraph) {
	if len(sg.nodes) == 0 {
		return
	}

	minX := 1000000
	minY := 1000000
	maxX := -1000000
	maxY := -1000000

	for _, child := range sg.children {
		self.calculateSubgraphBoundingBox(child)
		if len(child.nodes) > 0 {
			minX = Min(minX, child.minX)
			minY = Min(minY, child.minY)
			maxX = Max(maxX, child.maxX)
			maxY = Max(maxY, child.maxY)
		}
	}

	for _, node := range sg.nodes {
		if node.drawingCoord == nil || node.drawing == nil {
			continue
		}

		nodeMinX := node.drawingCoord.x
		nodeMinY := node.drawingCoord.y
		nodeMaxX := nodeMinX + len(*node.drawing) - 1
		nodeMaxY := nodeMinY + len((*node.drawing)[0]) - 1

		minX = Min(minX, nodeMinX)
		minY = Min(minY, nodeMinY)
		maxX = Max(maxX, nodeMaxX)
		maxY = Max(maxY, nodeMaxY)
	}

	currentWidth := maxX - minX
	currentInnerWidth := currentWidth + 3
	if currentInnerWidth < sg.label.width {
		extraWidth := sg.label.width - currentInnerWidth
		minX -= extraWidth / 2
		maxX += extraWidth - (extraWidth / 2)
	}

	const subgraphPadding = 2
	subgraphLabelSpace := sg.label.contentHeight() + 1
	sg.minX = minX - subgraphPadding
	sg.minY = minY - subgraphPadding - subgraphLabelSpace
	sg.maxX = maxX + subgraphPadding
	sg.maxY = maxY + subgraphPadding
}

func (self *graph) offsetDrawingForSubgraphs() {
	if len(self.subgraphs) == 0 {
		return
	}

	minX := 0
	minY := 0
	for _, sg := range self.subgraphs {
		minX = Min(minX, sg.minX)
		minY = Min(minY, sg.minY)
	}

	offsetX := -minX
	offsetY := -minY

	if offsetX == 0 && offsetY == 0 {
		return
	}

	self.offsetX = offsetX
	self.offsetY = offsetY

	for _, sg := range self.subgraphs {
		sg.minX += offsetX
		sg.minY += offsetY
		sg.maxX += offsetX
		sg.maxY += offsetY
	}

	for _, n := range self.nodes {
		if n.drawingCoord != nil {
			n.drawingCoord.x += offsetX
			n.drawingCoord.y += offsetY
		}
	}
}

func (self *graph) draw() *drawing {
	self.drawSubgraphs()

	for _, node := range self.nodes {
		if !node.drawn {
			self.drawNode(node)
		}
	}
	lineDrawings := []*drawing{}
	cornerDrawings := []*drawing{}
	arrowHeadDrawings := []*drawing{}
	boxStartDrawings := []*drawing{}
	labelDrawings := []*drawing{}
	for _, edge := range self.edges {
		line, boxStart, arrowHead, corners, label := self.drawEdge(edge)
		lineDrawings = append(lineDrawings, line)
		cornerDrawings = append(cornerDrawings, corners)
		arrowHeadDrawings = append(arrowHeadDrawings, arrowHead)
		boxStartDrawings = append(boxStartDrawings, boxStart)
		labelDrawings = append(labelDrawings, label)
	}

	self.drawing = self.mergeDrawings(self.drawing, drawingCoord{0, 0}, lineDrawings...)
	self.drawing = self.mergeDrawings(self.drawing, drawingCoord{0, 0}, cornerDrawings...)
	self.drawing = self.mergeDrawings(self.drawing, drawingCoord{0, 0}, arrowHeadDrawings...)
	self.drawing = self.mergeDrawings(self.drawing, drawingCoord{0, 0}, boxStartDrawings...)
	self.drawing = self.mergeDrawings(self.drawing, drawingCoord{0, 0}, labelDrawings...)

	self.drawSubgraphLabels()

	return self.drawing
}

func (self *graph) drawSubgraphs() {
	sortedSubgraphs := self.sortSubgraphsByDepth()

	for _, sg := range sortedSubgraphs {
		sgDrawing := drawSubgraph(sg, *self)
		offset := drawingCoord{sg.minX, sg.minY}
		self.drawing = self.mergeDrawings(self.drawing, offset, sgDrawing)
	}
}

func (self *graph) drawSubgraphLabels() {
	for _, sg := range self.subgraphs {
		if len(sg.nodes) == 0 {
			continue
		}
		labelDrawing, offset := drawSubgraphLabel(sg)
		self.drawing = self.mergeDrawings(self.drawing, offset, labelDrawing)
	}
}

func (self *graph) sortSubgraphsByDepth() []*subgraph {
	depths := make(map[*subgraph]int)
	for _, sg := range self.subgraphs {
		depths[sg] = self.getSubgraphDepth(sg)
	}

	sortedSubgraphs := make([]*subgraph, len(self.subgraphs))
	copy(sortedSubgraphs, self.subgraphs)

	for i := range sortedSubgraphs {
		for j := i + 1; j < len(sortedSubgraphs); j++ {
			if depths[sortedSubgraphs[i]] > depths[sortedSubgraphs[j]] {
				sortedSubgraphs[i], sortedSubgraphs[j] = sortedSubgraphs[j], sortedSubgraphs[i]
			}
		}
	}

	return sortedSubgraphs
}

func (self *graph) getSubgraphDepth(sg *subgraph) int {
	if sg.parent == nil {
		return 0
	}
	return 1 + self.getSubgraphDepth(sg.parent)
}

func (self *graph) getNode(nodeName string) (*node, error) {
	for _, n := range self.nodes {
		if n.name == nodeName {
			return n, nil
		}
	}
	return &node{}, errors.New("node " + nodeName + " not found")
}

func (self *graph) appendNode(n *node) {
	self.nodes = append(self.nodes, n)
}

func (self *graph) getEdgesFromNode(n *node) []edge {
	edges := []edge{}
	for _, edge := range self.edges {
		if edge.from.name == n.name {
			edges = append(edges, *edge)
		}
	}
	return edges
}

func (self *graph) getChildren(n *node) []*node {
	edges := self.getEdgesFromNode(n)
	children := []*node{}
	for _, edge := range edges {
		if edge.from.name == n.name {
			children = append(children, edge.to)
		}
	}
	return children
}

func (self *graph) gridToDrawingCoord(coordinate gridCoord) drawingCoord {
	x := 0
	y := 0
	for column := range coordinate.x {
		x += self.columnWidth[column]
	}
	for row := range coordinate.y {
		y += self.rowHeight[row]
	}
	return drawingCoord{
		x: x + self.columnWidth[coordinate.x]/2 + self.offsetX,
		y: y + self.rowHeight[coordinate.y]/2 + self.offsetY,
	}
}
