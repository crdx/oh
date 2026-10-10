package activitySpinner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/spinner"
	"crdx.org/oh/internal/app/style"
)

var _ segment.Refresher = state{}

const (
	defaultIdle = "·✦·"
	defaultRate = 100 * time.Millisecond
)

var defaultFrames = []string{"✦··", "·✦·", "··✦", "··✦", "·✦·", "✦··"}

type state struct {
	isRunning func() bool
	now       func() time.Time
	animation spinner.Animation
	idle      string
}

func New(isRunning func() bool, now func() time.Time) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Idle   *string        `toml:"idle"`
			Frames *[]string      `toml:"frames"`
			Rate   *time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		frames, idle, err := chosenFrames(args.Frames, args.Idle)
		if err != nil {
			return nil, err
		}

		rate := defaultRate
		if args.Rate != nil {
			rate = *args.Rate
		}
		if rate <= 0 {
			return nil, fmt.Errorf("rate must be positive (got %s)", rate)
		}

		width := style.Width(idle)
		for _, frame := range frames {
			if style.Width(frame) != width {
				return nil, fmt.Errorf(
					"%q is %d cells wide and %q is %d, so the rule beside them would shift",
					frame, style.Width(frame), idle, width,
				)
			}
		}

		return state{
			isRunning: isRunning,
			now:       now,
			animation: spinner.Of(rate, frames...),
			idle:      idle,
		}, nil
	}
}

func chosenFrames(writtenFrames *[]string, writtenIdle *string) ([]string, string, error) {
	if writtenFrames == nil {
		if writtenIdle == nil {
			return defaultFrames, defaultIdle, nil
		}
		return defaultFrames, *writtenIdle, nil
	}
	if len(*writtenFrames) == 0 {
		return nil, "", errors.New("frames must not be empty; leave them out for the default spinner")
	}
	if writtenIdle == nil {
		return *writtenFrames, strings.Repeat(" ", style.Width((*writtenFrames)[0])), nil
	}
	return *writtenFrames, *writtenIdle, nil
}

func (self state) NextRefresh(phase segment.Phase) time.Time {
	if !phase.IsRunning {
		return time.Time{}
	}

	interval := self.animation.RefreshInterval()

	return phase.At.Truncate(interval).Add(interval)
}

func (self state) Render(segment.Context) string {
	if !self.isRunning() {
		return style.Dim(self.idle)
	}

	return style.Spinner(self.animation.Frame(self.frameIndex()))
}

func (self state) frameIndex() int {
	interval := self.animation.RefreshInterval()

	return int(self.now().UnixNano() / interval.Nanoseconds())
}
