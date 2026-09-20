package fastMode

import (
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
)

const (
	FastMark     = "⚡"
	StandardMark = "·"
)

type state struct {
	isFast bool
}

func New(isFast bool) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{isFast: isFast}, nil
	}
}

func GetMark(isFast bool) string {
	if isFast {
		return style.Accent(FastMark)
	}

	return style.Subtle(StandardMark)
}

func (self state) Render(segment.Context) string {
	return GetMark(self.isFast)
}
