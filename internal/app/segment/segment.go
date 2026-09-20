package segment

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/schedule"
)

type Position int

const (
	TopLeft Position = iota
	TopCenter
	TopRight
	BottomLeft
	BottomCenter
	BottomRight
)

var Positions = []Position{
	TopLeft,
	TopCenter,
	TopRight,
	BottomLeft,
	BottomCenter,
	BottomRight,
}

var positionNames = map[Position]string{
	TopLeft:      "top.left",
	TopCenter:    "top.center",
	TopRight:     "top.right",
	BottomLeft:   "bottom.left",
	BottomCenter: "bottom.center",
	BottomRight:  "bottom.right",
}

func (self Position) String() string {
	return positionNames[self]
}

type Context struct {
	HiddenLinesAbove int
	HiddenLinesBelow int
}

type Segment interface {
	Render(context Context) string
}

type Fitter interface {
	RenderWithin(context Context, cells int) string
}

type Options interface {
	Read(into any) error
}

type Instance struct {
	Name    string
	Segment Segment
}

func (self Instance) Render(context Context) string {
	return self.Segment.Render(context)
}

type (
	Factory  func(Options) (Segment, error)
	Registry map[string]Factory
	Layout   map[Position][]Segment
)

func (self Registry) Build(name string, position Position, options Options) (Segment, error) {
	buildSegment, ok := self[name]
	if !ok {
		return nil, fmt.Errorf(
			"%s: there is no segment called %q, only: %s",
			position,
			name,
			strings.Join(self.Available(), ", "),
		)
	}

	segment, err := buildSegment(options)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", position, name, err)
	}

	return segment, nil
}

func (self Registry) Available() []string {
	return slices.Sorted(maps.Keys(self))
}

type Phase struct {
	At        time.Time
	IsRunning bool
}

type Refresher interface {
	NextRefresh(phase Phase) time.Time
}

func (self Layout) NextRefresh(phase Phase) time.Time {
	var due []time.Time

	for _, instances := range self {
		for _, instance := range instances {
			if namedInstance, isNamed := instance.(Instance); isNamed {
				instance = namedInstance.Segment
			}
			if refresher, ok := instance.(Refresher); ok {
				due = append(due, refresher.NextRefresh(phase))
			}
		}
	}

	return schedule.Soonest(due...)
}
