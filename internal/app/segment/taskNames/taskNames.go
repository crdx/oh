package taskNames

import (
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/schedule"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/tasks"
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
	getTasks  func() []tasks.Snapshot
	getRoutes func() []portgrant.Route
	now       func() time.Time
}

func New(
	getTasks func() []tasks.Snapshot,
	getRoutes func() []portgrant.Route,
	now func() time.Time,
) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{getTasks: getTasks, getRoutes: getRoutes, now: now}, nil
	}
}

func (self state) Render(segment.Context) string {
	var marks []string

	listing := self.getWork()
	slices.SortStableFunc(listing, func(left tasks.Snapshot, right tasks.Snapshot) int {
		if isLive(left.State) == isLive(right.State) {
			return 0
		}
		if isLive(left.State) {
			return -1
		}

		return 1
	})

	associatedWork := self.associatedWork()
	for _, item := range listing {
		if associatedWork[item.ID] || !self.isShown(item) {
			continue
		}

		if mark := describe(item); mark != "" {
			marks = append(marks, mark)
		}
	}

	return strings.Join(marks, " ")
}

func (self state) NextRefresh(segment.Phase) time.Time {
	now := self.now()

	var due []time.Time

	associatedWork := self.associatedWork()
	for _, item := range self.getWork() {
		if associatedWork[item.ID] {
			continue
		}
		if isLive(item.State) {
			return now.Add(beat)
		}

		if self.isShown(item) {
			due = append(due, item.EndedAt.Add(ghostLife))
		}
	}

	return schedule.Soonest(due...)
}

func (self state) getWork() []tasks.Snapshot {
	if self.getTasks == nil {
		return nil
	}

	return self.getTasks()
}

func (self state) associatedWork() map[string]bool {
	associatedWork := make(map[string]bool)
	for _, route := range self.getRoutes() {
		if route.TaskName != "" {
			associatedWork[route.TaskName] = true
		}
	}

	return associatedWork
}

func (self state) isShown(item tasks.Snapshot) bool {
	if isLive(item.State) {
		return true
	}

	return !item.EndedAt.IsZero() && self.now().Sub(item.EndedAt) < ghostLife
}

func isLive(state tasks.State) bool {
	return state == tasks.Queued || state == tasks.Running || state == tasks.Stopping
}

func describe(item tasks.Snapshot) string {
	var mark string
	switch item.State {
	case tasks.Queued, tasks.Running, tasks.Stopping:
		mark = liveMark
	case tasks.Failed, tasks.TimedOut:
		mark = failedMark
	case tasks.Done, tasks.Stopped:
		mark = finishedMark
	default:
		return ""
	}

	return RenderState(item.State, mark+" "+item.ID)
}

func RenderState(state tasks.State, text string) string {
	switch state {
	case tasks.Queued, tasks.Running:
		return style.Info(text)
	case tasks.Stopping:
		return style.Change(text)
	case tasks.Failed, tasks.TimedOut:
		return style.Failure(text)
	case tasks.Done, tasks.Stopped:
		return style.Dim(text)
	}

	return style.Dim(text)
}
