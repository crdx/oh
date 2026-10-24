package taskNames

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/schedule"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/tasks"
)

const (
	beat             = time.Second
	ghostLife        = 30 * time.Second
	maximumTaskNames = 3
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

type taskMark struct {
	icon  string
	label string
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

func (self state) Render(context segment.Context) string {
	ladder := self.Ladder(context)
	if len(ladder) == 0 {
		return ""
	}

	return ladder[0]
}

func (self state) Ladder(segment.Context) []string {
	marks := self.marks()

	return fit.DetailsAtMost(len(marks), maximumTaskNames, func(namedCount int) []string {
		parts := make([]string, 0, len(marks))
		for i, mark := range marks {
			if i < namedCount {
				parts = append(parts, mark.label)
			} else {
				parts = append(parts, mark.icon)
			}
		}

		return []string{strings.Join(parts, " ")}
	})
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

func (self state) marks() []taskMark {
	var marks []taskMark

	listing := self.getWork()
	slices.SortStableFunc(listing, func(left tasks.Snapshot, right tasks.Snapshot) int {
		return cmp.Compare(taskStateOrder(left.State), taskStateOrder(right.State))
	})

	associatedWork := self.associatedWork()
	for _, item := range listing {
		if associatedWork[item.ID] || !self.isShown(item) {
			continue
		}

		if mark := describe(item); mark.label != "" {
			marks = append(marks, mark)
		}
	}

	return marks
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

func taskStateOrder(state tasks.State) int {
	switch state {
	case tasks.Running:
		return 0
	case tasks.Queued:
		return 1
	case tasks.Stopping:
		return 2
	case tasks.Failed, tasks.TimedOut:
		return 3
	case tasks.Done, tasks.Stopped:
		return 4
	default:
		return 5
	}
}

func isLive(state tasks.State) bool {
	return state == tasks.Queued || state == tasks.Running || state == tasks.Stopping
}

func describe(item tasks.Snapshot) taskMark {
	var mark string
	switch item.State {
	case tasks.Queued, tasks.Running, tasks.Stopping:
		mark = liveMark
	case tasks.Failed, tasks.TimedOut:
		mark = failedMark
	case tasks.Done, tasks.Stopped:
		mark = finishedMark
	default:
		return taskMark{}
	}

	return taskMark{
		icon:  RenderState(item.State, mark),
		label: RenderState(item.State, mark+" "+item.ID),
	}
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
