package mermaid

type axis int

const (
	horizontalAxis axis = 1 << iota
	verticalAxis
)

type occupant struct {
	edge *edge
	axes axis
}

func axisBetween(from gridCoord, to gridCoord) axis {
	if from.y == to.y {
		return horizontalAxis
	}
	return verticalAxis
}

func canShareCells(first *edge, second *edge) bool {
	if first.isBidirectional || second.isBidirectional {
		return false
	}
	return first.from == second.from || first.to == second.to
}

func (self *graph) pathPenalty(route *edge, path []gridCoord) int {
	penalty := 0
	for index := 1; index < len(path); index++ {
		penalty += self.overlapPenalty(route, path[index-1], path[index])
	}
	return penalty
}

func (self *graph) overlapPenalty(route *edge, current gridCoord, next gridCoord) int {
	stepAxis := axisBetween(current, next)
	return self.cellPenalty(route, current, stepAxis) + self.cellPenalty(route, next, stepAxis)
}

func (self *graph) cellPenalty(route *edge, coordinate gridCoord, stepAxis axis) int {
	penalty := 0
	for _, other := range self.occupants[coordinate] {
		if other.axes&stepAxis != 0 && !canShareCells(route, other.edge) {
			penalty++
		}
	}
	return penalty
}

func (self *graph) occupyCells(route *edge) {
	for index := 1; index < len(route.path); index++ {
		start := route.path[index-1]
		end := route.path[index]
		stepAxis := axisBetween(start, end)
		stepX := Sign(end.x - start.x)
		stepY := Sign(end.y - start.y)
		for coordinate := start; ; coordinate = (gridCoord{x: coordinate.x + stepX, y: coordinate.y + stepY}) {
			self.occupy(coordinate, route, stepAxis)
			if coordinate.Equals(end) {
				break
			}
		}
	}
}

func (self *graph) occupy(coordinate gridCoord, route *edge, stepAxis axis) {
	for index, other := range self.occupants[coordinate] {
		if other.edge == route {
			self.occupants[coordinate][index].axes |= stepAxis
			return
		}
	}
	self.occupants[coordinate] = append(self.occupants[coordinate], occupant{edge: route, axes: stepAxis})
}
