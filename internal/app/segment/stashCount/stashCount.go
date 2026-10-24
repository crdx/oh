package stashCount

import (
	"strconv"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

const mark = "🗃️"

type state struct {
	getCount func() int
}

func New(getCount func() int) segment.Factory {
	if getCount == nil {
		getCount = func() int { return 0 }
	}
	return func(segment.Options) (segment.Segment, error) {
		return state{getCount: getCount}, nil
	}
}

func (self state) Render(segment.Context) string {
	count := self.getCount()
	if count == 0 {
		return ""
	}

	return mark + " " + style.Info(strconv.Itoa(count))
}
