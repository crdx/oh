package question

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/waiting"
)

type RememberedAnswer struct {
	Key       string
	Option    Option
	ExpiresAt time.Time
}

type memoryGate struct {
	token      chan struct{}
	answer     *RememberedAnswer
	generation uint64
}

func (self *Broker) memoryGate(key string) *memoryGate {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.memories == nil {
		self.memories = make(map[string]*memoryGate)
	}
	gate := self.memories[key]
	if gate == nil {
		gate = &memoryGate{token: make(chan struct{}, 1)}
		self.memories[key] = gate
	}
	return gate
}

func (self *Broker) ask(
	ctx context.Context,
	question Question,
	lapseAfter time.Duration,
	hasLapse bool,
) (answer, error) {
	question.Options = slices.Clone(question.Options)
	question.Fields = slices.Clone(question.Fields)
	for _, option := range question.Options {
		if option.RememberFor < 0 {
			return answer{index: noChoice}, errors.New("a question option has a negative memory duration")
		}
		if option.RememberFor > 0 && option.RememberForSession {
			return answer{index: noChoice}, errors.New("a question option cannot remember both a duration and the session")
		}
	}
	if question.Key == "" {
		if question.HasRememberableAnswers() {
			return answer{index: noChoice}, errors.New("a question with remembered answers needs a key")
		}
		return self.enqueue(ctx, question, lapseAfter, hasLapse)
	}
	finishWaiting := waiting.Begin(ctx)
	defer finishWaiting()

	gate := self.memoryGate(question.Key)
	select {
	case gate.token <- struct{}{}:
		defer func() { <-gate.token }()
	case <-ctx.Done():
		return answer{index: noChoice}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return answer{index: noChoice}, err
	}
	self.mutex.Lock()
	isInteractive := self.isInteractive
	generation := gate.generation
	index := gate.index(question, time.Now())
	self.mutex.Unlock()
	if !isInteractive {
		return answer{index: noChoice}, ErrUnavailable
	}
	if index != noChoice {
		return answer{index: index}, nil
	}

	result, err := self.enqueue(ctx, question, lapseAfter, hasLapse)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return answer{index: noChoice}, err
	}
	if result.isForSession || !result.until.IsZero() {
		self.mutex.Lock()
		if gate.generation == generation {
			gate.answer = &RememberedAnswer{Key: question.Key, Option: question.Options[result.index], ExpiresAt: result.until}
		}
		self.mutex.Unlock()
	}
	return result, nil
}

func (self *memoryGate) isActive(at time.Time) bool {
	return self.answer != nil && (self.answer.ExpiresAt.IsZero() || at.Before(self.answer.ExpiresAt))
}

func (self *memoryGate) index(question Question, at time.Time) int {
	if !self.isActive(at) {
		return noChoice
	}
	index := slices.Index(question.Options, self.answer.Option)
	if index == noChoice || slices.Contains(question.Options[index+1:], self.answer.Option) {
		self.answer = nil
		return noChoice
	}
	return index
}

func (self *Broker) RememberedAnswers() []RememberedAnswer {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	at := time.Now()
	var answers []RememberedAnswer
	for _, gate := range self.memories {
		if gate.isActive(at) {
			answers = append(answers, *gate.answer)
		}
	}
	slices.SortFunc(answers, func(first RememberedAnswer, second RememberedAnswer) int {
		return strings.Compare(first.Key, second.Key)
	})
	return answers
}

func (self *Broker) Forget(key string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	gate := self.memories[key]
	if gate == nil {
		return false
	}
	hadAnswer := gate.isActive(time.Now())
	gate.answer = nil
	gate.generation++
	return hadAnswer
}
