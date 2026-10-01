package notification

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/toolbox/notify"
)

const detailWidth = 80

func SendTurnError(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
	workspace *work.Space,
	failure error,
) error {
	_, err := notify.SendIfUnfocused(ctx, writeEscape, isTerminalFocused, notify.Args{
		Title:   title(workspace),
		Message: failure.Error(),
		Icon:    "error",
	}, notify.ExpiresAsUsual)
	return err
}

func SendQuestion(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
	workspace *work.Space,
	question ask.Question,
) (notify.Notification, error) {
	return notify.SendIfUnfocused(ctx, writeEscape, isTerminalFocused, notify.Args{
		Title:   title(workspace),
		Message: questionMessage(question),
		Icon:    "question",
	}, notify.NeverExpires)
}

type Questions struct {
	writeEscape         notify.EscapeWriter
	isTerminalFocused   func() bool
	workspace           *work.Space
	mutex               sync.Mutex
	standingWithdrawals map[int]func()
	nextIdentity        int
	withdrawalsInFlight sync.WaitGroup
}

func NewQuestions(
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
	workspace *work.Space,
) *Questions {
	return &Questions{
		writeEscape:         writeEscape,
		isTerminalFocused:   isTerminalFocused,
		workspace:           workspace,
		standingWithdrawals: map[int]func(){},
	}
}

func (self *Questions) Announce(question ask.Question) func() {
	sentNotification := make(chan notify.Notification, 1)
	go func() {
		notice, _ := SendQuestion(context.Background(), self.writeEscape, self.isTerminalFocused, self.workspace, question)
		sentNotification <- notice
	}()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	identity := self.nextIdentity
	self.nextIdentity++
	self.withdrawalsInFlight.Add(1)

	var once sync.Once
	withdraw := func() {
		once.Do(func() {
			self.mutex.Lock()
			delete(self.standingWithdrawals, identity)
			self.mutex.Unlock()

			go func() {
				defer self.withdrawalsInFlight.Done()
				_ = (<-sentNotification).Withdraw(context.Background(), self.writeEscape)
			}()
		})
	}
	self.standingWithdrawals[identity] = withdraw

	return withdraw
}

func (self *Questions) WithdrawAll(within time.Duration) {
	self.mutex.Lock()
	withdrawalsLeft := slices.Collect(maps.Values(self.standingWithdrawals))
	self.mutex.Unlock()

	for _, withdraw := range withdrawalsLeft {
		withdraw()
	}

	allSettled := make(chan struct{})
	go func() {
		self.withdrawalsInFlight.Wait()
		close(allSettled)
	}()

	select {
	case <-allSettled:
	case <-time.After(within):
	}
}

func title(workspace *work.Space) string {
	return "oh — " + workspace.GetName()
}

func questionMessage(question ask.Question) string {
	detail := questionDetail(question)
	if detail == "" {
		return question.Label
	}

	return question.Label + "\n" + detail
}

func questionDetail(question ask.Question) string {
	if question.Detail != "" {
		return detailLine(question.Detail)
	}
	if len(question.Fields) == 0 {
		return ""
	}

	detail := question.Fields[0].Name + ": " + question.Fields[0].Value
	if len(question.Fields) > 1 {
		detail += "\n" + question.Fields[1].Name
	}

	return detailLine(detail)
}

func detailLine(detail string) string {
	line, rest, _ := strings.Cut(strings.TrimSpace(detail), "\n")
	if strings.TrimSpace(rest) != "" {
		line += " " + width.Ellipsis
	}

	return width.Elide(line, detailWidth)
}
