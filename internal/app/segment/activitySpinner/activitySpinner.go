package activitySpinner

import (
	"errors"
	"fmt"
	"time"

	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/spinner"
	"crdx.org/io/internal/app/style"
)

var _ segment.Refresher = state{}

type state struct {
	isRunning func() bool
	now       func() time.Time
	animation spinner.Animation
	idle      string
}

func New(isRunning func() bool, now func() time.Time) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Idle   string        `toml:"idle"`
			Frames []string      `toml:"frames"`
			Rate   time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if len(args.Frames) == 0 {
			return nil, errors.New("frames must not be empty")
		}

		if args.Rate <= 0 {
			return nil, fmt.Errorf("rate must be positive (got %s)", args.Rate)
		}

		width := style.Width(args.Idle)
		for _, frame := range args.Frames {
			if style.Width(frame) != width {
				return nil, fmt.Errorf(
					"%q is %d cells wide and %q is %d, so the rule beside them would shift",
					frame, style.Width(frame), args.Idle, width,
				)
			}
		}

		return state{
			isRunning: isRunning,
			now:       now,
			animation: spinner.Of(args.Rate, args.Frames...),
			idle:      args.Idle,
		}, nil
	}
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
