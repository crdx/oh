package ask_test

import (
	"context"
	"errors"
	"testing"

	"crdx.org/io/pkg/ask"
)

func answerWith(t *testing.T, broker *ask.Broker, answer func(request *ask.Request)) {
	t.Helper()

	go func() {
		<-broker.Changes()
		if request := broker.Current(); request != nil {
			answer(request)
		}
	}()
}

func TestConfirmationReadsItsAnswer(t *testing.T) {
	for name, test := range map[string]struct {
		answer  func(request *ask.Request)
		wantErr error
	}{
		"yes":       {answer: func(request *ask.Request) { request.Choose(0) }},
		"no":        {answer: func(request *ask.Request) { request.Choose(1) }, wantErr: ask.ErrDenied},
		"cancelled": {answer: (*ask.Request).Cancel, wantErr: ask.ErrDenied},
	} {
		t.Run(name, func(t *testing.T) {
			broker := ask.New()
			closeBroker := broker.Open()
			defer closeBroker()

			answerWith(t, broker, test.answer)

			err := ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"})
			if !errors.Is(err, test.wantErr) {
				t.Errorf("got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestConfirmationReportsWhyItCouldNotBeAsked(t *testing.T) {
	broker := ask.New()

	err := ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"})
	if !errors.Is(err, ask.ErrUnavailable) {
		t.Errorf("got %v, want the question to be unavailable", err)
	}
}

func TestConfirmationCarriesCancellationThrough(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-broker.Changes()
		cancel()
	}()

	err := ask.Confirm(ctx, broker, ask.Confirmation{Label: "Continue?"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancellation", err)
	}
}

func TestConfirmationOffersYesAndNoAndPrefersYes(t *testing.T) {
	question := ask.Confirmation{Label: "Continue?", Detail: "rm -rf /", Language: "bash"}.Question()

	if len(question.Options) != 2 {
		t.Fatalf("got %d options, want two", len(question.Options))
	}
	if question.Options[0].Key != 'y' || question.Options[1].Key != 'n' {
		t.Errorf("got keys %q and %q, want y and n", question.Options[0].Key, question.Options[1].Key)
	}
	if question.DefaultIndex() != 0 {
		t.Errorf("got default %d, want the approving option", question.DefaultIndex())
	}
	if question.Detail != "rm -rf /" || question.Language != "bash" {
		t.Errorf("got detail %q in %q", question.Detail, question.Language)
	}
}

func TestAChoiceNumbersTheFirstNineOptions(t *testing.T) {
	labels := make([]string, 0, 10)
	for range 10 {
		labels = append(labels, "option")
	}

	question := ask.Choice{Label: "Which one?", Labels: labels}.Question()

	if len(question.Options) != 10 {
		t.Fatalf("got %d options, want ten", len(question.Options))
	}
	if question.Options[0].Key != '1' || question.Options[8].Key != '9' {
		t.Errorf("got keys %q and %q, want 1 and 9", question.Options[0].Key, question.Options[8].Key)
	}
	if question.Options[9].Key != 0 {
		t.Errorf("got key %q for the tenth option, want none", question.Options[9].Key)
	}
	if question.DefaultIndex() != 0 {
		t.Errorf("got default %d, want the first option", question.DefaultIndex())
	}
}

func TestAKeyFindsTheOptionItChooses(t *testing.T) {
	question := ask.Confirmation{Label: "Continue?"}.Question()

	if index := question.IndexForKey('n'); index != 1 {
		t.Errorf("got index %d for n, want the denying option", index)
	}
	if index := question.IndexForKey('q'); index != -1 {
		t.Errorf("got index %d for an unoffered key, want none", index)
	}
}

func TestADefaultNobodyWasOfferedFallsToTheFirstOption(t *testing.T) {
	question := ask.Question{Options: []ask.Option{{Label: "only"}}, Default: 4}

	if question.DefaultIndex() != 0 {
		t.Errorf("got default %d, want the first option", question.DefaultIndex())
	}
}
