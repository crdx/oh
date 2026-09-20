package ask_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/ask"
)

func TestAQuestionIsUnavailableOutsideInteractiveMode(t *testing.T) {
	broker := ask.New()

	if _, err := broker.Ask(t.Context(), ask.Question{}); !errors.Is(err, ask.ErrUnavailable) {
		t.Errorf("got %v, want somebody to be required", err)
	}
}

func TestAnAnswerSettlesOneRequest(t *testing.T) {
	for name, test := range map[string]struct {
		answer    func(request *ask.Request)
		wantIndex int
		wantErr   error
	}{
		"first option":  {answer: func(request *ask.Request) { request.Choose(0) }},
		"second option": {answer: func(request *ask.Request) { request.Choose(1) }, wantIndex: 1},
		"cancelled":     {answer: (*ask.Request).Cancel, wantIndex: -1, wantErr: ask.ErrCancelled},
	} {
		t.Run(name, func(t *testing.T) {
			broker := ask.New()
			closeBroker := broker.Open()
			defer closeBroker()

			question := ask.Confirmation{
				Label:    "Run this command?",
				Detail:   "curl example.com",
				Language: "bash",
			}.Question()

			result := make(chan int, 1)
			failure := make(chan error, 1)
			go func() {
				index, err := broker.Ask(t.Context(), question)
				result <- index
				failure <- err
			}()

			<-broker.Changes()
			request := broker.Current()
			if request == nil {
				t.Fatal("the request was immediately cleared")
			}
			if !reflect.DeepEqual(request.Question, question) {
				t.Errorf("got question %+v, want %+v", request.Question, question)
			}
			test.answer(request)

			if index := <-result; index != test.wantIndex {
				t.Errorf("got index %d, want %d", index, test.wantIndex)
			}
			if err := <-failure; !errors.Is(err, test.wantErr) {
				t.Errorf("got %v, want %v", err, test.wantErr)
			}
			if request := broker.Current(); request != nil {
				t.Errorf("the answered question remained current: %+v", request.Question)
			}
		})
	}
}

func TestAnOptionNobodyWasOfferedIsIgnored(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	go func() { _, _ = broker.Ask(t.Context(), ask.Confirmation{Label: "Continue?"}.Question()) }()
	<-broker.Changes()

	request := broker.Current()
	request.Choose(7)

	if broker.Current() != request {
		t.Error("an option outside the offered ones answered the question")
	}
}

func TestConcurrentRequestsReachTheAnswererInOrder(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	firstResult := make(chan error, 1)
	go func() {
		_, err := broker.Ask(t.Context(), ask.Confirmation{Label: "First?"}.Question())
		firstResult <- err
	}()
	<-broker.Changes()

	secondResult := make(chan error, 1)
	go func() {
		_, err := broker.Ask(t.Context(), ask.Confirmation{Label: "Second?"}.Question())
		secondResult <- err
	}()
	<-broker.Changes()

	first := broker.Current()
	if first == nil || first.Question.Label != "First?" {
		t.Fatalf("got first request %+v", first)
	}
	first.Choose(0)
	if err := <-firstResult; err != nil {
		t.Fatalf("the first request failed: %v", err)
	}

	second := broker.Current()
	if second == nil || second.Question.Label != "Second?" {
		t.Fatalf("got second request %+v", second)
	}
	second.Cancel()
	if err := <-secondResult; !errors.Is(err, ask.ErrCancelled) {
		t.Errorf("got %v, want the second request to be cancelled", err)
	}
}

func TestCancellationWithdrawsARequest(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := broker.Ask(ctx, ask.Confirmation{Label: "Continue?"}.Question())
		result <- err
	}()
	<-broker.Changes()

	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancellation", err)
	}
	if request := broker.Current(); request != nil {
		t.Errorf("the cancelled question remained current: %+v", request.Question)
	}
}

func TestARequestCarriesTheDeadlineItWasAskedUnder(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	go func() { _, _ = broker.Ask(ctx, ask.Confirmation{Label: "Continue?"}.Question()) }()
	<-broker.Changes()

	wanted, _ := ctx.Deadline()
	deadline, hasDeadline := broker.Current().Deadline()
	if !hasDeadline || !deadline.Equal(wanted) {
		t.Errorf("got deadline %v (%t), want %v", deadline, hasDeadline, wanted)
	}
}

func TestARequestWithoutADeadlineReportsNone(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	go func() { _, _ = broker.Ask(t.Context(), ask.Confirmation{Label: "Continue?"}.Question()) }()
	<-broker.Changes()

	if _, hasDeadline := broker.Current().Deadline(); hasDeadline {
		t.Error("a question asked without a deadline reported a deadline")
	}
}

func TestAQuestionRecordsHowLongItStoodAgainstWhoeverAskedIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const stood = time.Minute

		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()

		ctx, waitedTime := waiting.Track(t.Context())

		answered := make(chan struct{})
		go func() {
			defer close(answered)
			_, _ = broker.Ask(ctx, ask.Confirmation{Label: "Continue?"}.Question())
		}()
		<-broker.Changes()

		time.Sleep(stood)
		broker.Current().Choose(0)
		<-answered

		if got := waitedTime(); got != stood {
			t.Errorf("got %s, want the whole time the question stood", got)
		}
	})
}

func TestAQuestionNobodyIsTimingIsAnsweredAllTheSame(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	answer := make(chan int, 1)
	failure := make(chan error, 1)
	go func() {
		index, err := broker.Ask(t.Context(), ask.Confirmation{Label: "Continue?"}.Question())
		answer <- index
		failure <- err
	}()
	<-broker.Changes()

	broker.Current().Choose(0)

	if err := <-failure; err != nil {
		t.Fatalf("got %v, want the question answered", err)
	}
	if index := <-answer; index != 0 {
		t.Errorf("got option %d, want the first", index)
	}
}

func TestAQuestionCutShortStillRecordsWhatItTook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const stood = 30 * time.Second

		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ctx, waitedTime := waiting.Track(ctx)

		refused := make(chan error, 1)
		go func() {
			_, err := broker.Ask(ctx, ask.Confirmation{Label: "Continue?"}.Question())
			refused <- err
		}()
		<-broker.Changes()

		time.Sleep(stood)
		cancel()
		<-refused

		if got := waitedTime(); got != stood {
			t.Errorf("got %s, want the time it stood before it was cut short", got)
		}
	})
}
