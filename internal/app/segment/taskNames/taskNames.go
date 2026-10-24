package taskNames

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/schedule"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/tasks"
)

const (
	beat             = time.Second
	finishedTaskLife = 10 * time.Second
	maximumTaskNames = 3
)

const (
	liveMark   = "●"
	emptyMark  = "○"
	failedMark = "✗"
)

var _ segment.Refresher = state{}

type state struct {
	sessionName string
	getTasks    func() []tasks.Snapshot
	getRoutes   func() []portgrant.Route
	now         func() time.Time
}

type taskMark struct {
	icon  string
	label string
}

func New(
	sessionName string,
	getTasks func() []tasks.Snapshot,
	getRoutes func() []portgrant.Route,
	now func() time.Time,
) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{sessionName: sessionName, getTasks: getTasks, getRoutes: getRoutes, now: now}, nil
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
			due = append(due, now.Add(beat))
			flashEndsAt := item.ActivityAt.Add(tasks.ActivityFlash)
			if !item.ActivityAt.IsZero() && flashEndsAt.After(now) {
				due = append(due, flashEndsAt)
			}
			continue
		}

		if isShownAt(item, now) {
			due = append(due, item.EndedAt.Add(finishedTaskLife))
		}
	}

	return schedule.Soonest(due...)
}

func (self state) marks() []taskMark {
	var marks []taskMark

	now := self.now()
	listing := self.getWork()
	slices.SortStableFunc(listing, func(left tasks.Snapshot, right tasks.Snapshot) int {
		return cmp.Compare(taskStateOrder(left.State), taskStateOrder(right.State))
	})

	associatedWork := self.associatedWork()
	for _, item := range listing {
		if associatedWork[item.ID] || !isShownAt(item, now) {
			continue
		}

		if mark := describe(item, now, self.sessionName); mark.label != "" {
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

func isShownAt(item tasks.Snapshot, now time.Time) bool {
	if isLive(item.State) {
		return true
	}

	return !item.EndedAt.IsZero() && now.Sub(item.EndedAt) < finishedTaskLife
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

func describe(item tasks.Snapshot, now time.Time, sessionName string) taskMark {
	var mark string
	switch item.State {
	case tasks.Queued, tasks.Running, tasks.Stopping:
		mark = liveMark
	case tasks.Failed, tasks.TimedOut:
		mark = failedMark
	case tasks.Done, tasks.Stopped:
		mark = emptyMark
	default:
		return taskMark{}
	}

	icon := RenderState(item.State, mark)
	if isLive(item.State) && !item.ActivityAt.IsZero() && now.Before(item.ActivityAt.Add(tasks.ActivityFlash)) {
		icon = style.Accent(mark)
	}
	label := RenderState(item.State, mark+" "+item.ID)
	if isLive(item.State) {
		label = icon + " " + RenderState(item.State, item.ID)
	}
	if sessionName == "" || item.Run < 1 {
		return taskMark{icon: icon, label: label}
	}

	address := toolresult.TaskURL(toolresult.TaskReference{
		EndedAt:     item.EndedAt,
		SessionName: sessionName,
		TaskName:    item.ID,
		Run:         item.Run,
	})
	return taskMark{
		icon:  link.RenderURL(icon, address),
		label: link.RenderURL(label, address),
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
