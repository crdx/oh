package er

import (
	"math"
	"strings"

	"crdx.org/oh/internal/mermaid/runewidth"
)

type canvas struct {
	rows [][]string
}

func (self *canvas) ensure(x int, y int) {
	for len(self.rows) <= y {
		self.rows = append(self.rows, nil)
	}
	for len(self.rows[y]) <= x {
		self.rows[y] = append(self.rows[y], " ")
	}
}

func (self *canvas) set(x int, y int, cell string) {
	if x < 0 || y < 0 {
		return
	}
	self.ensure(x, y)
	self.rows[y][x] = cell
}

func (self *canvas) at(x int, y int) string {
	if y < 0 || y >= len(self.rows) || x < 0 || x >= len(self.rows[y]) {
		return " "
	}
	return self.rows[y][x]
}

func (self *canvas) stamp(x0 int, y0 int, block []string) {
	for dy, line := range block {
		for x, cell := range runewidth.Cells(line) {
			self.set(x0+x, y0+dy, cell)
		}
	}
}

func (self *canvas) string() string {
	var b strings.Builder
	for _, row := range self.rows {
		b.WriteString(strings.TrimRight(strings.Join(row, ""), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

type side int

const (
	sideT side = iota
	sideB
)

type placedEntity struct {
	entity   *Entity
	lines    []string
	x, y     int
	w, h     int
	row, col int
}

type layout struct {
	byName         map[string]*placedEntity
	placedEntities []*placedEntity
	lanes          int
	gutW           int
	vGutX          []int
	hGutY          []int
}

func placeEntities(diagram *ErDiagram, glyphSet glyphs) *layout {
	entityCount := len(diagram.Entities)
	cols := max(int(math.Ceil(math.Sqrt(float64(entityCount)))), 1)
	rows := (entityCount + cols - 1) / cols

	lanes := max(len(diagram.Relationships), 1)

	maxLabel := 0
	for _, r := range diagram.Relationships {
		if w := runewidth.StringWidth(r.Label); w > maxLabel {
			maxLabel = w
		}
	}
	gutW := lanes + maxLabel + 5

	deg := map[string]int{}
	selfLoop := map[string]bool{}
	for _, r := range diagram.Relationships {
		deg[r.Left]++
		deg[r.Right]++
		if r.Left == r.Right {
			selfLoop[r.Left] = true
		}
	}

	placedEntities := make([]*placedEntity, entityCount)
	for i, entity := range diagram.Entities {
		minW := 4*deg[entity.Name] + 1
		if selfLoop[entity.Name] {
			minW = 11
			if deg[entity.Name] > 2 {
				minW = 4*deg[entity.Name] + 9
			}
		}
		lines := renderEntity(entity, glyphSet, minW-2)
		placedEntities[i] = &placedEntity{
			entity: entity, lines: lines,
			w: blockWidth(lines), h: len(lines),
			row: i / cols, col: i % cols,
		}
	}

	colW := make([]int, cols)
	rowH := make([]int, rows)
	for _, p := range placedEntities {
		if p.w > colW[p.col] {
			colW[p.col] = p.w
		}
		if p.h > rowH[p.row] {
			rowH[p.row] = p.h
		}
	}

	vGutX := make([]int, cols+1)
	colX := make([]int, cols)
	x := 0
	for column := range cols {
		vGutX[column] = x
		if column > 0 {
			x += gutW
			colX[column] = x
		} else {
			colX[0] = 0
		}
		x += colW[column]
	}
	vGutX[cols] = x

	hGutY := make([]int, rows+1)
	rowY := make([]int, rows)
	y := 0
	for row := range rows {
		hGutY[row] = y
		if row > 0 {
			y += lanes
			rowY[row] = y
		} else {
			rowY[0] = 0
		}
		y += rowH[row]
	}
	hGutY[rows] = y

	byName := map[string]*placedEntity{}
	for _, p := range placedEntities {
		p.x, p.y = colX[p.col], rowY[p.row]
		byName[p.entity.Name] = p
	}
	return &layout{byName: byName, placedEntities: placedEntities, lanes: lanes, gutW: gutW, vGutX: vGutX, hGutY: hGutY}
}

func blockWidth(lines []string) int {
	width := 0
	for _, l := range lines {
		if lw := runewidth.StringWidth(l); lw > width {
			width = lw
		}
	}
	return width
}

const (
	dN uint8 = 1 << iota
	dS
	dE
	dW
)

type overlay struct {
	solid map[[2]int]uint8
	dash  map[[2]int]uint8
	label map[[2]int]string
	token map[[2]int]string
}

func newOverlay() *overlay {
	return &overlay{
		solid: map[[2]int]uint8{}, dash: map[[2]int]uint8{},
		label: map[[2]int]string{}, token: map[[2]int]string{},
	}
}

func (self *overlay) bits(x int, y int) uint8 {
	return self.solid[[2]int{x, y}] | self.dash[[2]int{x, y}]
}

func (self *overlay) polyline(pts [][2]int, isSolid bool) {
	marks := self.dash
	if isSolid {
		marks = self.solid
	}
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		dx, dy := sign(b[0]-a[0]), sign(b[1]-a[1])
		x, y := a[0], a[1]
		for x != b[0] || y != b[1] {
			if dx > 0 {
				marks[[2]int{x, y}] |= dE
			} else if dx < 0 {
				marks[[2]int{x, y}] |= dW
			}
			if dy > 0 {
				marks[[2]int{x, y}] |= dS
			} else if dy < 0 {
				marks[[2]int{x, y}] |= dN
			}
			x += dx
			y += dy
			if dx > 0 {
				marks[[2]int{x, y}] |= dW
			} else if dx < 0 {
				marks[[2]int{x, y}] |= dE
			}
			if dy > 0 {
				marks[[2]int{x, y}] |= dN
			} else if dy < 0 {
				marks[[2]int{x, y}] |= dS
			}
		}
	}
}

func attach(placedEntity *placedEntity, attachSide side, index int, total int) (int, int) {
	lo, hi := placedEntity.x+1, placedEntity.x+placedEntity.w-2
	x := lo + (hi-lo-4*(total-1))/2 + 4*index
	if (attachSide == sideB) == (x%2 != 0) {
		x++
	}
	x = max(lo, min(x, hi))
	y := placedEntity.y + placedEntity.h - 1
	if attachSide == sideT {
		y = placedEntity.y
	}
	return x, y
}

type endpoint struct {
	p    *placedEntity
	s    side
	x, y int
	card Cardinality
}

func drawConnectors(grid *canvas, lay *layout, diagram *ErDiagram, glyphSet glyphs) {
	layer := newOverlay()

	type ends struct{ a, b endpoint }
	all := make([]ends, len(diagram.Relationships))
	slotCount := map[[2]int]int{}
	entityIndex := map[*placedEntity]int{}
	for i, p := range lay.placedEntities {
		entityIndex[p] = i
	}
	for i, relationship := range diagram.Relationships {
		left, right := lay.byName[relationship.Left], lay.byName[relationship.Right]
		if left == nil || right == nil {
			continue
		}
		sa, sb := sidesFor(left, right)
		all[i] = ends{
			a: endpoint{p: left, s: sa, card: relationship.LeftCard},
			b: endpoint{p: right, s: sb, card: relationship.RightCard},
		}
		slotCount[[2]int{entityIndex[left], int(sa)}]++
		slotCount[[2]int{entityIndex[right], int(sb)}]++
	}
	slotUsed := map[[2]int]int{}
	selfSeen := map[*placedEntity]int{}
	for i := range all {
		if all[i].a.p == nil {
			continue
		}
		for _, ep := range []*endpoint{&all[i].a, &all[i].b} {
			key := [2]int{entityIndex[ep.p], int(ep.s)}
			ep.x, ep.y = attach(ep.p, ep.s, slotUsed[key], slotCount[key])
			slotUsed[key]++
		}
		if p := all[i].a.p; p == all[i].b.p {
			in := 4 * selfSeen[p]
			selfSeen[p]++
			x1, x2 := p.x+1+in, p.x+p.w-2-in
			if x1%2 != 0 {
				x1++
			}
			if x2%2 != 0 {
				x2--
			}
			all[i].a.x, all[i].b.x = x1, x2
		}
	}

	plans := make([]routePlan, 0, len(diagram.Relationships))
	for i, r := range diagram.Relationships {
		if all[i].a.p == nil {
			continue
		}
		plans = append(plans, newPlan(lay, all[i].a, all[i].b, r, i))
	}

	for _, p := range plans {
		p.drawLine(layer)
	}
	for _, p := range plans {
		p.decorate(layer)
	}

	composite(grid, layer, glyphSet)

	for _, p := range plans {
		setAttachTee(grid, p.a, glyphSet)
		setAttachTee(grid, p.b, glyphSet)
	}
}

func setAttachTee(grid *canvas, ep endpoint, glyphSet glyphs) {
	tee, opposite := glyphSet.teeD, glyphSet.teeU
	if ep.s == sideT {
		tee, opposite = glyphSet.teeU, glyphSet.teeD
	}
	if grid.at(ep.x, ep.y) == string(opposite) {
		tee = glyphSet.cross
	}
	grid.set(ep.x, ep.y, string(tee))
}

func sidesFor(first *placedEntity, second *placedEntity) (side, side) {
	isSameColAdjacent := first.col == second.col && abs(first.row-second.row) == 1
	switch {
	case first.row < second.row:
		if isSameColAdjacent {
			return sideB, sideB
		}
		return sideB, sideT
	case first.row > second.row:
		if isSameColAdjacent {
			return sideB, sideB
		}
		return sideT, sideB
	default:
		return sideB, sideB
	}
}

func (self *layout) gutterY(e endpoint, lane int) int {
	if e.s == sideB {
		return self.hGutY[e.p.row+1] + lane
	}
	return self.hGutY[e.p.row] + lane
}

func (self *layout) trunkX(a *placedEntity, b *placedEntity, lane int) int {
	return self.vGutX[min(a.col, b.col)+1] + self.gutW - self.lanes + lane
}

type routePlan struct {
	rel      *Relationship
	a, b     endpoint
	ya, yb   int
	tx       int
	isMerged bool
}

func newPlan(lay *layout, a endpoint, b endpoint, r *Relationship, lane int) routePlan {
	ya, yb := lay.gutterY(a, lane), lay.gutterY(b, lane)
	p := routePlan{rel: r, a: a, b: b, ya: ya, yb: yb, isMerged: ya == yb}
	if !p.isMerged {
		p.tx = lay.trunkX(a.p, b.p, lane)
	}
	return p
}

func (self routePlan) drawLine(layer *overlay) {
	if self.isMerged {
		layer.polyline([][2]int{
			{self.a.x, self.a.y}, {self.a.x, self.ya}, {self.b.x, self.ya}, {self.b.x, self.b.y},
		}, self.rel.Identifying)
		return
	}
	layer.polyline([][2]int{
		{self.a.x, self.a.y}, {self.a.x, self.ya}, {self.tx, self.ya}, {self.tx, self.yb}, {self.b.x, self.yb}, {self.b.x, self.b.y},
	}, self.rel.Identifying)
}

func (self routePlan) decorate(layer *overlay) {
	if self.isMerged {
		putToken(layer, self.a, self.b.x, self.ya)
		putToken(layer, self.b, self.a.x, self.ya)
		if self.a.p == self.b.p {
			writeLabel(layer, self.rel.Label, max(self.a.x, self.b.x)+2, self.ya, -1)
		} else {
			putLabel(layer, self.rel.Label, [][3]int{{min(self.a.x, self.b.x), max(self.a.x, self.b.x), self.ya}})
		}
		return
	}
	putToken(layer, self.a, self.tx, self.ya)
	putToken(layer, self.b, self.tx, self.yb)
	runs := [][3]int{
		{min(self.a.x, self.tx), max(self.a.x, self.tx), self.ya},
		{min(self.b.x, self.tx), max(self.b.x, self.tx), self.yb},
	}
	if runs[1][1]-runs[1][0] > runs[0][1]-runs[0][0] {
		runs[0], runs[1] = runs[1], runs[0]
	}
	putLabel(layer, self.rel.Label, runs)
}

func putToken(layer *overlay, ep endpoint, targetX int, y int) {
	if ep.x < targetX {
		for i, cell := range runewidth.Cells(leftToken(ep.card)) {
			layer.token[[2]int{ep.x + 1 + i, y}] = cell
		}
		return
	}
	tokenCells := runewidth.Cells(rightToken(ep.card))
	for i, cell := range tokenCells {
		layer.token[[2]int{ep.x - len(tokenCells) + i, y}] = cell
	}
}

func putLabel(layer *overlay, label string, runs [][3]int) {
	if label == "" {
		return
	}
	lw := runewidth.StringWidth(label)
	type spot struct{ start, cost, y, hi int }
	best := spot{cost: -1}
	for _, r := range runs {
		lo, hi, y := r[0]+3, r[1]-3, r[2]
		if hi-lo+1 < lw {
			continue
		}
		start, cost := labelStart(layer, lo, hi, lw, y)
		if best.cost < 0 || cost < best.cost {
			best = spot{start, cost, y, hi}
		}
		if cost == 0 {
			break
		}
	}
	if best.cost < 0 {
		lo, hi, y := runs[0][0]+3, runs[0][1]-3, runs[0][2]
		if lo > hi {
			return
		}
		start, _ := labelStart(layer, lo, hi, lw, y)
		best = spot{start, 0, y, hi}
	}
	writeLabel(layer, label, best.start, best.y, best.hi)
}

func labelStart(layer *overlay, lo int, hi int, lw int, y int) (int, int) {
	centre := max(lo, lo+(hi-lo+1-lw)/2)
	start, cost := centre, vCrossings(layer, centre, min(centre+lw-1, hi), y)
	for d := 1; d <= hi-lo && cost > 0; d++ {
		for _, c := range []int{centre - d, centre + d} {
			if c < lo || c+lw-1 > hi {
				continue
			}
			if n := vCrossings(layer, c, c+lw-1, y); n < cost {
				start, cost = c, n
			}
		}
	}
	return start, cost
}

func vCrossings(o *overlay, x0 int, x1 int, y int) int {
	crossings := 0
	for x := x0; x <= x1; x++ {
		if o.bits(x, y)&(dN|dS) != 0 {
			crossings++
		}
	}
	return crossings
}

func writeLabel(layer *overlay, label string, x int, y int, limit int) {
	for _, cell := range runewidth.Cells(label) {
		if limit >= 0 && x > limit {
			return
		}
		if cell != " " || layer.bits(x, y)&(dN|dS) == 0 {
			layer.label[[2]int{x, y}] = cell
		}
		x++
	}
}

func composite(grid *canvas, layer *overlay, glyphSet glyphs) {
	seen := map[[2]int]bool{}
	mark := func(x, y int) {
		point := [2]int{x, y}
		if seen[point] {
			return
		}
		seen[point] = true
		bits := layer.bits(x, y)
		if bits == 0 {
			return
		}
		if grid.at(x, y) != " " {
			return
		}
		grid.set(x, y, string(glyphFor(bits, layer.solid[point] != 0, glyphSet)))
	}
	for p := range layer.solid {
		mark(p[0], p[1])
	}
	for p := range layer.dash {
		mark(p[0], p[1])
	}
	for p, r := range layer.label {
		grid.set(p[0], p[1], r)
	}
	for p, r := range layer.token {
		grid.set(p[0], p[1], r)
	}
}

func glyphFor(bits uint8, isSolid bool, glyphSet glyphs) rune {
	switch bits {
	case dN | dS:
		if isSolid {
			return glyphSet.v
		}
		return glyphSet.vd
	case dE | dW:
		if isSolid {
			return glyphSet.h
		}
		return glyphSet.hd
	case dN | dE:
		return glyphSet.bl
	case dN | dW:
		return glyphSet.br
	case dS | dE:
		return glyphSet.tl
	case dS | dW:
		return glyphSet.tr
	case dN | dS | dE:
		return glyphSet.teeR
	case dN | dS | dW:
		return glyphSet.teeL
	case dN | dE | dW:
		return glyphSet.teeU
	case dS | dE | dW:
		return glyphSet.teeD
	case dN | dS | dE | dW:
		return glyphSet.cross
	case dN, dS:
		if isSolid {
			return glyphSet.v
		}
		return glyphSet.vd
	default:
		if isSolid {
			return glyphSet.h
		}
		return glyphSet.hd
	}
}

func leftToken(c Cardinality) string {
	switch c {
	case OnlyOne:
		return "||"
	case ZeroOrOne:
		return "|o"
	case ZeroOrMore:
		return "}o"
	case OneOrMore:
		return "}|"
	default:
		return "}|"
	}
}

func rightToken(c Cardinality) string {
	switch c {
	case OnlyOne:
		return "||"
	case ZeroOrOne:
		return "o|"
	case ZeroOrMore:
		return "o{"
	case OneOrMore:
		return "|{"
	default:
		return "|{"
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func sign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}
