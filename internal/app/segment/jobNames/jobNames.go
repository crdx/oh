package jobNames

import (
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/schedule"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/jobs"
)

const (
	beat      = time.Second
	ghostLife = 30 * time.Second
)

const (
	liveMark     = "●"
	finishedMark = "○"
	failedMark   = "✗"
)

var _ segment.Refresher = state{}

type state struct {
	getJobs func() []jobs.Snapshot
	now     func() time.Time
}

func New(getJobs func() []jobs.Snapshot, now func() time.Time) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{getJobs: getJobs, now: now}, nil
	}
}

func (self state) Render(segment.Context) string {
	var marks []string

	listing := slices.Clone(self.getJobs())
	slices.SortStableFunc(listing, func(left jobs.Snapshot, right jobs.Snapshot) int {
		if left.IsLive() == right.IsLive() {
			return 0
		}
		if left.IsLive() {
			return -1
		}

		return 1
	})

	for _, snapshot := range listing {
		if !self.isShown(snapshot) {
			continue
		}

		if mark := describe(snapshot); mark != "" {
			marks = append(marks, mark)
		}
	}

	return strings.Join(marks, " ")
}

func (self state) NextRefresh(segment.Phase) time.Time {
	now := self.now()

	var due []time.Time

	for _, snapshot := range self.getJobs() {
		if snapshot.IsLive() {
			return now.Add(beat)
		}

		if self.isShown(snapshot) {
			due = append(due, snapshot.EndedAt.Add(ghostLife))
		}
	}

	return schedule.Soonest(due...)
}

func (self state) isShown(snapshot jobs.Snapshot) bool {
	if snapshot.IsLive() {
		return true
	}

	return !snapshot.EndedAt.IsZero() && self.now().Sub(snapshot.EndedAt) < ghostLife
}

func describe(snapshot jobs.Snapshot) string {
	switch snapshot.State {
	case jobs.StateStarting, jobs.StateRunning:
		return style.Info(liveMark + " " + snapshot.Name)
	case jobs.StateStopping:
		return style.Change(liveMark + " " + snapshot.Name)
	case jobs.StateFailed:
		return style.Failure(failedMark + " " + snapshot.Name)
	case jobs.StateComplete, jobs.StateStopped, jobs.StateEnded:
		return style.Dim(finishedMark + " " + snapshot.Name)
	}

	return ""
}
