package question_test

import (
	"context"
	"errors"
	"testing"

	"crdx.org/oh/pkg/question"
)

func answerWith(t *testing.T, broker *question.Broker, answer func(request *question.Request)) {
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
		answer  func(request *question.Request)
		wantErr error
	}{
		"yes":       {answer: func(request *question.Request) { request.Choose(0) }},
		"no":        {answer: func(request *question.Request) { request.Choose(1) }, wantErr: question.ErrDenied},
		"cancelled": {answer: (*question.Request).Cancel, wantErr: question.ErrDenied},
	} {
		t.Run(name, func(t *testing.T) {
			broker := question.NewBroker()
			closeBroker := broker.Open()
			defer closeBroker()

			answerWith(t, broker, test.answer)

			err := question.Confirm(t.Context(), broker, question.Confirmation{Label: "Continue?"})
			if !errors.Is(err, test.wantErr) {
				t.Errorf("got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestConfirmationReportsWhyItCouldNotBeAsked(t *testing.T) {
	broker := question.NewBroker()

	err := question.Confirm(t.Context(), broker, question.Confirmation{Label: "Continue?"})
	if !errors.Is(err, question.ErrUnavailable) {
		t.Errorf("got %v, want the question to be unavailable", err)
	}
}

func TestConfirmationCarriesCancellationThrough(t *testing.T) {
	broker := question.NewBroker()
	closeBroker := broker.Open()
	defer closeBroker()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-broker.Changes()
		cancel()
	}()

	err := question.Confirm(ctx, broker, question.Confirmation{Label: "Continue?"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancellation", err)
	}
}

func TestConfirmationOffersYesAndNoAndPrefersYes(t *testing.T) {
	confirmation := question.Confirmation{
		Label:    "Continue?",
		Detail:   "rm -rf /",
		Language: "bash",
		Fields:   []question.Field{{Name: "target", Value: "/"}},
	}.Question()

	if len(confirmation.Options) != 2 {
		t.Fatalf("got %d options, want two", len(confirmation.Options))
	}
	if confirmation.Options[0].Key != 'y' || confirmation.Options[1].Key != 'n' {
		t.Errorf("got keys %q and %q, want y and n", confirmation.Options[0].Key, confirmation.Options[1].Key)
	}
	if confirmation.DefaultIndex() != 0 {
		t.Errorf("got default %d, want the approving option", confirmation.DefaultIndex())
	}
	if confirmation.Detail != "rm -rf /" || confirmation.Language != "bash" {
		t.Errorf("got detail %q in %q", confirmation.Detail, confirmation.Language)
	}
	if len(confirmation.Fields) != 1 || confirmation.Fields[0] != (question.Field{Name: "target", Value: "/"}) {
		t.Errorf("got fields %+v", confirmation.Fields)
	}
}

func TestAChoiceNumbersTheFirstNineOptions(t *testing.T) {
	labels := make([]string, 0, 10)
	for range 10 {
		labels = append(labels, "option")
	}

	question := question.Choice{Label: "Which one?", Labels: labels}.Question()

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
	question := question.Confirmation{Label: "Continue?"}.Question()

	if index := question.IndexForKey('n'); index != 1 {
		t.Errorf("got index %d for n, want the denying option", index)
	}
	if index := question.IndexForKey('q'); index != -1 {
		t.Errorf("got index %d for an unoffered key, want none", index)
	}
}

func TestADefaultNobodyWasOfferedFallsToTheFirstOption(t *testing.T) {
	question := question.Question{Options: []question.Option{{Label: "only"}}, Default: 4}

	if question.DefaultIndex() != 0 {
		t.Errorf("got default %d, want the first option", question.DefaultIndex())
	}
}

func TestAnOptionWithoutAShortcutDoesNotMatchTheZeroKey(t *testing.T) {
	prompt := question.Question{Options: []question.Option{{Label: "Deny"}}}
	if index := prompt.IndexForKey(0); index != -1 {
		t.Fatalf("an absent shortcut selected option %d", index)
	}
}
