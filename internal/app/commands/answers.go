package commands

import (
	"fmt"
	"strings"
	"time"

	"crdx.org/oh/internal/app/revocation"
	"crdx.org/oh/internal/app/style"
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
		targets = append(targets, revocation.Target{Key: answer.Key, Description: "approval"})
	}
	return targets
}

func (self AnswerRevocations) Revoke(key string) (revocation.Result, error) {
	if self.Broker == nil || !self.Broker.Forget(key) {
		return revocation.Result{}, fmt.Errorf("no approval for %s", key)
	}
	return revocation.Result{Message: "Revoked the approval for " + key + "."}, nil
}

func formatRememberedAnswers(broker *question.Broker) string {
	if broker == nil {
		return ""
	}
	answers := broker.RememberedAnswers()
	if len(answers) == 0 {
		return ""
	}
	rows := []string{style.Info("Approved:")}
	for _, answer := range answers {
		lifetime := "until you quit"
		if !answer.ExpiresAt.IsZero() {
			remainingTime := max(time.Until(answer.ExpiresAt), time.Second)
			lifetime = util.CompactDuration(remainingTime.Round(time.Second)) + " remaining"
		}
		rows = append(rows, "  "+answer.Key+" — "+answer.Option.Label+" ("+lifetime+")")
	}
	return strings.Join(rows, "\n")
}
