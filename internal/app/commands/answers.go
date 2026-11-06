package commands

import (
	"fmt"
	"strings"
	"time"

	"crdx.org/oh/internal/app/revocation"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/question"
)

type AnswerRevocations struct {
	Broker *question.Broker
}

func (self AnswerRevocations) Targets() []revocation.Target {
	if self.Broker == nil {
		return nil
	}
	var targets []revocation.Target
	for _, answer := range self.Broker.RememberedAnswers() {
		targets = append(targets, revocation.Target{Key: answer.Key, Description: tr.ApprovalTarget})
	}
	return targets
}

func (self AnswerRevocations) Revoke(key string) (revocation.Result, error) {
	if self.Broker == nil || !self.Broker.Forget(key) {
		return revocation.Result{}, fmt.Errorf(tr.NoApproval, key)
	}
	return revocation.Result{Message: fmt.Sprintf(tr.ApprovalRevoked, key)}, nil
}

func formatRememberedAnswers(broker *question.Broker) string {
	if broker == nil {
		return ""
	}
	answers := broker.RememberedAnswers()
	if len(answers) == 0 {
		return ""
	}
	rows := []string{style.Info(tr.ApprovedHeading)}
	for _, answer := range answers {
		lifetime := tr.UntilQuitLifetime
		if !answer.ExpiresAt.IsZero() {
			remainingTime := max(time.Until(answer.ExpiresAt), time.Second)
			lifetime = fmt.Sprintf(tr.RemainingLifetime, util.CompactDuration(remainingTime.Round(time.Second)))
		}
		rows = append(rows, "  "+answer.Key+" — "+answer.Option.Label+" ("+lifetime+")")
	}
	return strings.Join(rows, "\n")
}
