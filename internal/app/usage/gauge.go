package usage

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"sync"

	"crdx.org/oh/internal/app/graphics"
	"crdx.org/oh/internal/app/style"
)

const (
	barPadding     = 5
	tickCells      = 8
	tickDivisor    = 2
	trackDivisor   = 2
	mostPlacements = 64
)

type Graphics struct {
	CellWidth  int
	CellHeight int
}

type Measure func() (Graphics, bool)

type Gauges struct {
	measure Measure

	mutex      sync.Mutex
	placements map[string]string
}

func NewGauges(measure Measure) *Gauges {
	return &Gauges{measure: measure, placements: map[string]string{}}
}

func TerminalGauges(input *os.File, output *os.File) *Gauges {
	cellWidth, cellHeight, hasGraphics := graphics.Detect(input, output)
	if !hasGraphics {
		return NewGauges(nil)
	}

	return NewGauges(func() (Graphics, bool) {
		if width, height := graphics.CellSize(output); width > 0 && height > 0 {
			return Graphics{CellWidth: width, CellHeight: height}, true
		}

		return Graphics{CellWidth: cellWidth, CellHeight: cellHeight}, true
	})
}

func FixedGauges(drawing Graphics) *Gauges {
	return NewGauges(func() (Graphics, bool) { return drawing, true })
}

func (self *Gauges) Draw(usedPercent int, expectedPercent *int, pace Pace, cells int) string {
	if self != nil && self.measure != nil {
		if drawing, hasGraphics := self.measure(); hasGraphics {
			if placement, isPlaced := self.place(usedPercent, expectedPercent, pace, cells, drawing); isPlaced {
				return placement
			}
		}
	}

	return blockGauge(usedPercent, expectedPercent, pace, cells)
}

func (self *Gauges) place(
	usedPercent int, expectedPercent *int, pace Pace, cells int, drawing Graphics,
) (string, bool) {
	key := gaugeKey(usedPercent, expectedPercent, pace, cells, drawing)

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if placement, isKnown := self.placements[key]; isKnown {
		return placement, true
	}

	placement, isPlaced := graphics.Place(
		gaugeImage(usedPercent, expectedPercent, pace, cells, drawing), cells,
	)
	if !isPlaced {
		return "", false
	}

	if len(self.placements) >= mostPlacements {
		clear(self.placements)
	}

	self.placements[key] = placement

	return placement, true
}

func gaugeKey(usedPercent int, expectedPercent *int, pace Pace, cells int, drawing Graphics) string {
	pacePercent := -1
	if expectedPercent != nil {
		pacePercent = *expectedPercent
	}

	return fmt.Sprintf(
		"%d/%d/%d/%d/%dx%d",
		usedPercent, pacePercent, pace, cells, drawing.CellWidth, drawing.CellHeight,
	)
}

func gaugeImage(usedPercent int, expectedPercent *int, pace Pace, cells int, drawing Graphics) *image.RGBA {
	pixelWidth := cells * drawing.CellWidth
	pixelHeight := drawing.CellHeight

	picture := image.NewRGBA(image.Rect(0, 0, pixelWidth, pixelHeight))

	padding := pixelHeight / barPadding
	fillWidth := usedPercent * pixelWidth / percentCeiling

	tickColumn := -1
	tickHalf := max(1, drawing.CellWidth/tickCells)

	if expectedPercent != nil {
		tickColumn = *expectedPercent * pixelWidth / percentCeiling
	}

	fill := paceColour(pace)
	track := scale(style.DimColour(), trackDivisor)

	for y := padding; y < pixelHeight-padding; y++ {
		for x := range pixelWidth {
			isFilled := x < fillWidth

			pixel := track
			if isFilled {
				pixel = fill
			}

			if tickColumn >= 0 && absolute(x-tickColumn) <= tickHalf {
				pixel = style.DimColour()
				if isFilled {
					pixel = scale(fill, tickDivisor)
				}
			}

			picture.SetRGBA(x, y, pixel)
		}
	}

	return picture
}

func paceColour(pace Pace) color.RGBA {
	switch pace {
	case PaceAhead:
		return style.ChangeColour()
	case PaceCritical:
		return style.FailureColour()
	case PaceEven:
	}

	return style.InformationColour()
}

func scale(value color.RGBA, divisor uint8) color.RGBA {
	return color.RGBA{
		R: value.R / divisor,
		G: value.G / divisor,
		B: value.B / divisor,
		A: value.A,
	}
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
