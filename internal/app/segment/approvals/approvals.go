package approvals

import (
	"strconv"
	"time"

	"crdx.org/oh/internal/app/schedule"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/question"
)

const (
	minutesInHour = 60
	minuteMark    = "m"
	hourMark      = "h"
)

var _ segment.Refresher = state{}

type state struct {
	getApprovals func() []question.RememberedAnswer
	now          func() time.Time
}

type lifetime struct {
	text      string
	changesAt time.Time
}

func New(getApprovals func() []question.RememberedAnswer, now func() time.Time) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}
		return state{getApprovals: getApprovals, now: now}, nil
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
	now := self.now()

	var parts []string
	for _, approval := range self.activeApprovals(now) {
		part := style.Normal(approval.Key)
		if !approval.ExpiresAt.IsZero() {
			part += " " + style.Dim(lifetimeOf(approval.ExpiresAt, now).text)
		}
		parts = append(parts, part)
	}

	return fit.Parts(parts)
}

func (self state) NextRefresh(segment.Phase) time.Time {
	now := self.now()

	var due []time.Time
	for _, approval := range self.activeApprovals(now) {
		if !approval.ExpiresAt.IsZero() {
			due = append(due, lifetimeOf(approval.ExpiresAt, now).changesAt)
		}
	}

	return schedule.Soonest(due...)
}

func (self state) activeApprovals(now time.Time) []question.RememberedAnswer {
	if self.getApprovals == nil {
		return nil
	}

	var approvals []question.RememberedAnswer
	for _, approval := range self.getApprovals() {
		if approval.ExpiresAt.IsZero() || now.Before(approval.ExpiresAt) {
			approvals = append(approvals, approval)
		}
	}

	return approvals
}

func lifetimeOf(expiresAt time.Time, now time.Time) lifetime {
	remainingTime := expiresAt.Sub(now)
	minutes := ceilingOf(remainingTime, time.Minute)
	if minutes < minutesInHour {
		return lifetime{
			text:      strconv.Itoa(minutes) + minuteMark,
			changesAt: expiresAt.Add(-time.Duration(minutes-1) * time.Minute),
		}
	}

	hours := ceilingOf(remainingTime, time.Hour)
	changingTime := max(time.Duration(hours-1)*time.Hour, (minutesInHour-1)*time.Minute)
	return lifetime{
		text:      strconv.Itoa(hours) + hourMark,
		changesAt: expiresAt.Add(-changingTime),
	}
}

func ceilingOf(duration time.Duration, unit time.Duration) int {
	return int((duration + unit - 1) / unit)
}
