package usage

import (
	"context"
	"time"

	"crdx.org/oh/pkg/agent"
)

type WindowSnapshot struct {
	Windows   []agent.UsageWindow
	FetchedAt time.Time
}

type WindowTracker struct {
	reporter       agent.UsageReporter
	snapshotter    Snapshotter
	sharedReporter *sharedReporter
	now            func() time.Time
}

func NewWindowTracker(
	reporter agent.UsageReporter, path string, ttl time.Duration, now func() time.Time,
) *WindowTracker {
	trackedReporter := Shared(reporter, path, ttl, now)
	snapshotter, _ := trackedReporter.(Snapshotter)
	sharedReporter, _ := trackedReporter.(*sharedReporter)

	return &WindowTracker{
		reporter:       trackedReporter,
		snapshotter:    snapshotter,
		sharedReporter: sharedReporter,
		now:            now,
	}
}

func (self *WindowTracker) IsAvailable() bool {
	return self.reporter != nil && self.reporter.IsAvailable()
}

func (self *WindowTracker) ReadSnapshot() WindowSnapshot {
	if self.sharedReporter != nil {
		windows, fetchedAt := self.sharedReporter.readSnapshot()

		return WindowSnapshot{Windows: windows, FetchedAt: fetchedAt}
	}

	if self.snapshotter == nil {
		return WindowSnapshot{}
	}

	windows, fetchedAt := self.snapshotter.GetSnapshot()

	return WindowSnapshot{Windows: windows, FetchedAt: fetchedAt}
}

func (self *WindowTracker) Fetch(ctx context.Context) (WindowSnapshot, error) {
	windows, err := self.reporter.UsageWindows(ctx)
	snapshot := WindowSnapshot{Windows: windows, FetchedAt: self.now()}

	if self.sharedReporter != nil {
		if sharedSnapshot := self.ReadSnapshot(); len(sharedSnapshot.Windows) > 0 {
			snapshot = sharedSnapshot
		}
	}

	return snapshot, err
}
