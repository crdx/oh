package question_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/question"
)

func startApproval(ctx context.Context, broker *question.Broker, scope string) <-chan error {
	result := make(chan error, 1)
	go func() {
		index, err := broker.AskWithTimeout(ctx, question.Question{
			Key:   scope,
			Label: scope,
			Options: []question.Option{
				{Label: "Allow once"},
				{Label: "Deny"},
				{Label: "Allow for two minutes", RememberFor: 2 * time.Minute},
				{Label: "Allow for session", RememberForSession: true},
			},
		}, time.Minute)
		if errors.Is(err, question.ErrCancelled) || err == nil && index == 1 {
			err = question.ErrDenied
		}
		result <- err
	}()
	return result
}

func TestTimedApprovalsCoverWaitingCallsAndExpireWithoutExtending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		first := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		second := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		broker.Current().Choose(2)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if broker.Current() != nil {
			t.Fatal("a waiting call asked again")
		}
		time.Sleep(time.Minute)
		if err := <-startApproval(t.Context(), broker, "fetch"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		third := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		if broker.Current() == nil {
			t.Fatal("the approval did not expire at its original deadline")
		}
		broker.Current().Choose(1)
		if err := <-third; !errors.Is(err, question.ErrDenied) {
			t.Fatalf("got %v, want denial", err)
		}
	})
}

func TestTimedApprovalsStayWithinTheirScopeAndBroker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		first := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		broker.Current().Choose(2)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		other := startApproval(t.Context(), broker, "lookup")
		synctest.Wait()
		if broker.Current() == nil {
			t.Fatal("a different scope inherited the approval")
		}
		broker.Current().Cancel()
		if err := <-other; !errors.Is(err, question.ErrDenied) {
			t.Fatal(err)
		}
		fresh := question.NewBroker()
		t.Cleanup(fresh.Open())
		again := startApproval(t.Context(), fresh, "fetch")
		synctest.Wait()
		if fresh.Current() == nil {
			t.Fatal("a new broker inherited the approval")
		}
		fresh.Current().Cancel()
		if err := <-again; !errors.Is(err, question.ErrDenied) {
			t.Fatal(err)
		}
	})
}

func TestOrdinaryAndInvalidAnswersNeverCreateTimedApprovals(t *testing.T) {
	for _, index := range []int{0, 1} {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				broker := question.NewBroker()
				t.Cleanup(broker.Open())
				first := startApproval(t.Context(), broker, "fetch")
				synctest.Wait()
				request := broker.Current()
				request.Choose(-1)
				request.Choose(10)
				if broker.Current() != request {
					t.Fatal("an invalid index answered the question")
				}
				request.Choose(index)
				if err := <-first; (err == nil) != (index == 0) {
					t.Fatal(err)
				}
				second := startApproval(t.Context(), broker, "fetch")
				synctest.Wait()
				if broker.Current() == nil {
					t.Fatal("an ordinary answer created a timed approval")
				}
				broker.Current().Cancel()
				if err := <-second; !errors.Is(err, question.ErrDenied) {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestApprovalGateWaitCanBeCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		first := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		second := startApproval(ctx, broker, "fetch")
		synctest.Wait()
		cancel()
		if err := <-second; !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want cancellation", err)
		}
		broker.Current().Cancel()
		if err := <-first; !errors.Is(err, question.ErrDenied) {
			t.Fatal(err)
		}
	})
}

func TestExpiredOrCancelledQuestionsCannotGrantTime(t *testing.T) {
	for _, shouldCancel := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			broker := question.NewBroker()
			t.Cleanup(broker.Open())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first := startApproval(ctx, broker, "fetch")
			synctest.Wait()
			request := broker.Current()
			if shouldCancel {
				cancel()
			} else {
				time.Sleep(time.Minute)
			}
			request.Choose(2)
			want := context.DeadlineExceeded
			if shouldCancel {
				want = context.Canceled
			}
			if err := <-first; !errors.Is(err, want) {
				t.Fatalf("got %v, want %v for a stale question", err, want)
			}
			second := startApproval(t.Context(), broker, "fetch")
			synctest.Wait()
			if broker.Current() == nil {
				t.Fatal("a stale question created a timed approval")
			}
			broker.Current().Cancel()
			if err := <-second; !errors.Is(err, question.ErrDenied) {
				t.Fatal(err)
			}
		})
	}
}

func TestRememberedChoicesFollowTheOptionAndCanBeForgotten(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		prompt := question.Question{
			Key:   "format",
			Label: "Choose a format",
			Options: []question.Option{
				{Key: 'p', Label: "Plain"},
				{Key: 'm', Label: "Markdown", RememberForSession: true},
			},
		}
		answers := make(chan int, 1)
		go func() {
			index, _ := broker.Ask(t.Context(), prompt)
			answers <- index
		}()
		synctest.Wait()
		broker.Current().Choose(1)
		if index := <-answers; index != 1 {
			t.Fatalf("got option %d, want Markdown", index)
		}
		time.Sleep(48 * time.Hour)
		prompt.Options[0], prompt.Options[1] = prompt.Options[1], prompt.Options[0]
		if index, err := broker.Ask(t.Context(), prompt); err != nil || index != 0 {
			t.Fatalf("reordered question returned %d, %v", index, err)
		}
		memory := broker.RememberedAnswers()
		if len(memory) != 1 || memory[0].Option.Label != "Markdown" || !memory[0].ExpiresAt.IsZero() {
			t.Fatalf("unexpected session memory: %+v", memory)
		}
		if !broker.Forget("format") || broker.Forget("format") {
			t.Fatal("forget did not remove exactly one remembered answer")
		}
		go func() {
			index, _ := broker.Ask(t.Context(), prompt)
			answers <- index
		}()
		synctest.Wait()
		if broker.Current() == nil {
			t.Fatal("the forgotten choice was reused")
		}
		broker.Current().Choose(1)
		<-answers
		if len(broker.RememberedAnswers()) != 0 {
			t.Fatal("a one-off choice was remembered")
		}
	})
}

func TestForgettingWhileAQuestionStandsPreventsItRestoringMemory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		result := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		broker.Forget("fetch")
		broker.Current().Choose(2)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		if len(broker.RememberedAnswers()) != 0 {
			t.Fatal("an answer restored forgotten memory")
		}
	})
}

func TestRememberedAnswersExpireAndRespectCancellationAndInteractivity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		closeBroker := broker.Open()
		first := startApproval(t.Context(), broker, "fetch")
		synctest.Wait()
		broker.Current().Choose(2)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := <-startApproval(ctx, broker, "fetch"); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call returned %v", err)
		}
		closeBroker()
		if err := <-startApproval(t.Context(), broker, "fetch"); !errors.Is(err, question.ErrUnavailable) {
			t.Fatalf("closed broker returned %v", err)
		}
		time.Sleep(2 * time.Minute)
		if len(broker.RememberedAnswers()) != 0 || broker.Forget("fetch") {
			t.Fatal("expired answers remained active")
		}
	})
}

func TestRememberingRequiresAKeyAndAnExplicitOption(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		prompt := question.Question{Label: "Continue?", Options: []question.Option{{Label: "Yes", RememberForSession: true}}}
		if _, err := broker.Ask(t.Context(), prompt); err == nil {
			t.Fatal("a remembering question without a key was accepted")
		}
		prompt.Key = "continue"
		prompt.Options[0].RememberForSession = false
		result := make(chan error, 1)
		go func() {
			_, err := broker.Ask(t.Context(), prompt)
			result <- err
		}()
		synctest.Wait()
		broker.Current().Choose(0)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		if len(broker.RememberedAnswers()) != 0 {
			t.Fatal("an ordinary option was remembered")
		}
	})
}

func TestDuplicateOptionsNeverReuseTheWrongAnswerIndex(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		option := question.Option{Key: 'y', Label: "Yes", RememberForSession: true}
		prompt := question.Question{Key: "duplicate", Options: []question.Option{option, option}}
		answers := make(chan int, 1)
		ask := func() {
			index, _ := broker.Ask(t.Context(), prompt)
			answers <- index
		}
		go ask()
		synctest.Wait()
		broker.Current().Choose(1)
		if index := <-answers; index != 1 {
			t.Fatalf("got %d, want the second option", index)
		}
		go ask()
		synctest.Wait()
		if broker.Current() == nil {
			t.Fatal("an ambiguous option reused a remembered index")
		}
		broker.Current().Cancel()
		<-answers
	})
}

func TestQuestionsOwnTheirOptionAndFieldSnapshots(t *testing.T) {
	for _, index := range []int{0, 1} {
		synctest.Test(t, func(t *testing.T) {
			broker := question.NewBroker()
			t.Cleanup(broker.Open())
			prompt := question.Question{
				Key: "snapshot", Label: "Continue?",
				Options: []question.Option{{Label: "Yes", RememberForSession: true}, {Label: "No"}},
				Fields:  []question.Field{{Name: "value", Value: "original"}},
			}
			result := make(chan error, 1)
			go func() {
				_, err := broker.Ask(t.Context(), prompt)
				result <- err
			}()
			synctest.Wait()
			prompt.Options[0].Label = "changed"
			prompt.Fields[0].Value = "changed"
			request := broker.Current()
			if request.Question.Options[0].Label != "Yes" || request.Question.Fields[0].Value != "original" {
				t.Fatal("caller-owned slices changed the question")
			}
			request.Question.Options[0].RememberForSession = false
			request.Question.Options[1].RememberForSession = true
			request.Choose(index)
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			memory := broker.RememberedAnswers()
			if index == 0 && (len(memory) != 1 || memory[0].Option.Label != "Yes") {
				t.Fatalf("the broker remembered mutated options: %+v", memory)
			}
			if index == 1 && len(memory) != 0 {
				t.Fatal("presentation changes gave an option a memory lifetime")
			}
		})
	}
}

func TestInvalidOptionLifetimesNeverReachTheInterface(t *testing.T) {
	for _, option := range []question.Option{
		{Label: "Negative", RememberFor: -time.Second},
		{Label: "Both", RememberFor: time.Minute, RememberForSession: true},
	} {
		broker := question.NewBroker()
		closeBroker := broker.Open()
		_, err := broker.Ask(t.Context(), question.Question{Key: "invalid", Options: []question.Option{option}})
		closeBroker()
		if err == nil || broker.Current() != nil || len(broker.RememberedAnswers()) != 0 {
			t.Fatalf("invalid option reached the interface or memory: %+v, %v", option, err)
		}
	}
}

func TestChangingAnOptionsLifetimeInvalidatesTheRememberedAnswer(t *testing.T) {
	for _, lifetime := range []time.Duration{0, 2 * time.Minute} {
		synctest.Test(t, func(t *testing.T) {
			broker := question.NewBroker()
			t.Cleanup(broker.Open())
			prompt := question.Question{Key: "policy", Options: []question.Option{{Label: "Continue", RememberFor: time.Minute}}}
			result := make(chan error, 1)
			ask := func() {
				_, err := broker.Ask(t.Context(), prompt)
				result <- err
			}
			go ask()
			synctest.Wait()
			broker.Current().Choose(0)
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			prompt.Options[0].RememberFor = lifetime
			go ask()
			synctest.Wait()
			if broker.Current() == nil || len(broker.RememberedAnswers()) != 0 {
				t.Fatal("a changed lifetime reused the old answer")
			}
			broker.Current().Cancel()
			if err := <-result; !errors.Is(err, question.ErrCancelled) {
				t.Fatal(err)
			}
		})
	}
}
