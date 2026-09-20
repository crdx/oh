package turnTimer

import (
	"strconv"
	"time"

	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/turn"
)

var _ segment.Refresher = state{}

const step = time.Minute

type state struct {
	getTiming     func() turn.Timing
	isTurnRunning func() bool
}

func New(getTiming func() turn.Timing, isTurnRunning func() bool) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{getTiming: getTiming, isTurnRunning: isTurnRunning}, nil
	}
}

func (self state) NextRefresh(phase segment.Phase) time.Time {
	timing := self.getTiming()
	active := timing.UserTurn
	if phase.IsRunning {
		active = timing.ModelTurn
	}
	return phase.At.Add(step - active%step)
}

func (self state) Render(segment.Context) string {
	timing := self.getTiming()
	isTurnRunning := self.isTurnRunning()
	return renderDuration(timing.UserTurn, !isTurnRunning) + " " + renderDuration(timing.ModelTurn, isTurnRunning)
}

func renderDuration(elapsedTime time.Duration, isActive bool) string {
	number := strconv.FormatInt(int64(elapsedTime/time.Minute), 10)
	if !isActive {
		return style.Dim(number + "m")
	}
	return style.Normal(number) + style.Dim("m")
}
