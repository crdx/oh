package subagentCounts

import (
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/pkg/session"
)

const ghostLife = 30 * time.Second

type state struct {
	get func() []subagents.Snapshot
	now func() time.Time
}

func New(get func() []subagents.Snapshot, now func() time.Time) segment.Factory {
	if get == nil {
		get = func() []subagents.Snapshot { return nil }
	}
	return func(segment.Options) (segment.Segment, error) {
		return state{get: get, now: now}, nil
	}
}

func (self state) Render(segment.Context) string {
	var labels []string
	doneCount, failureCount := 0, 0
	for _, snapshot := range self.get() {
		if snapshot.State.IsLive() {
			labels = append(labels, childMark(snapshot, self.now()))
			continue
		}
		if snapshot.EndedAt.IsZero() || self.now().Sub(snapshot.EndedAt) >= ghostLife {
			continue
		}
		if snapshot.State == subagentrecord.Failed {
			failureCount++
		} else {
			doneCount++
		}
	}
	if doneCount > 0 {
		labels = append(labels, style.Dim("○ "+strconv.Itoa(doneCount)))
	}
	if failureCount > 0 {
		labels = append(labels, style.Failure("✗ "+strconv.Itoa(failureCount)))
	}
	return strings.Join(labels, " ")
}

func childMark(snapshot subagents.Snapshot, now time.Time) string {
	mark := session.Emoji(snapshot.Name)
	if mark == "" {
		mark = style.Info("●")
	}
	if snapshot.ActivityAt.IsZero() || !now.Before(snapshot.ActivityAt.Add(subagents.ActivityFlash)) {
		mark = style.QuietSubagent(mark)
	}
	if snapshot.State == subagentrecord.Stopping {
		return mark + style.Change("…")
	}
	return mark
}

func (self state) NextRefresh(segment.Phase) time.Time {
	var due time.Time
	for _, snapshot := range self.get() {
		if snapshot.State.IsLive() && !snapshot.ActivityAt.IsZero() {
			expires := snapshot.ActivityAt.Add(subagents.ActivityFlash)
			if expires.After(self.now()) && (due.IsZero() || expires.Before(due)) {
				due = expires
			}
		}
		if snapshot.EndedAt.IsZero() {
			continue
		}
		expires := snapshot.EndedAt.Add(ghostLife)
		if expires.After(self.now()) && (due.IsZero() || expires.Before(due)) {
			due = expires
		}
	}
	return due
}
