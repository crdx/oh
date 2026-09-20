package spinner

import "time"

type Animation interface {
	Frame(index int) string
	RefreshInterval() time.Duration
}

var Activity Animation = Of(125*time.Millisecond, "✦·", "·✦", "·✧", "✧·")

type Frames struct {
	frames   []string
	interval time.Duration
}

func Of(interval time.Duration, frames ...string) Frames {
	return Frames{frames: frames, interval: interval}
}

func (self Frames) Frame(index int) string {
	return self.frames[index%len(self.frames)]
}

func (self Frames) RefreshInterval() time.Duration {
	return self.interval
}
