package localTime

import (
	"time"

	"crdx.org/io/internal/app/schedule"
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
)

var _ segment.Refresher = state{}

const defaultFormat = "15:04"

var reference = time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)

type state struct {
	now         func() time.Time
	format      string
	granularity time.Duration
}

func New(now func() time.Time) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Format string `toml:"format"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if args.Format == "" {
			args.Format = defaultFormat
		}

		return state{
			now:         now,
			format:      args.Format,
			granularity: granularityOf(args.Format),
		}, nil
	}
}

func (self state) NextRefresh(phase segment.Phase) time.Time {
	return schedule.NextTick(phase.At, self.granularity)
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.now().Format(self.format))
}

func granularityOf(format string) time.Duration {
	if reference.Format(format) != reference.Add(time.Second).Format(format) {
		return time.Second
	}

	return time.Minute
}
