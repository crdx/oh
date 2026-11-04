package commands

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/revocation"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/pkg/question"
)

func rememberAnswerForTest(t *testing.T, broker *question.Broker, key string, isForSession bool) {
	t.Helper()
	option := question.Option{Label: "Allow for 5 mins", RememberFor: 5 * time.Minute}
	if isForSession {
		option = question.Option{Label: "Allow for session", RememberForSession: true}
	}
	result := make(chan error, 1)
	go func() {
		_, err := broker.AskWithTimeout(t.Context(), question.Question{
			Key: key, Label: "Continue?", Options: []question.Option{option},
		}, time.Minute)
		result <- err
	}()
	synctest.Wait()
	broker.Current().Choose(0)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestRevokeListsAndForgetsAnswersAlongsidePathsAndPorts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		rememberAnswerForTest(t, broker, "tool:fetch", false)
		rememberAnswerForTest(t, broker, "tool:weather", true)
		grants, paths := fixturePathGrants()
		*paths = []pathgrant.Grant{{Path: "/tool:fetch", Access: pathgrant.ReadAccess}}
		ports, forwarded := fixturePortGrants()
		*forwarded = []uint16{8080}
		registry := newRevocations(grants, ports, []revocation.Source{AnswerRevocations{Broker: broker}})
		want := []string{"/tool:fetch", "8080", "tool:fetch", "tool:weather"}
		if got := revocableSubjects(registry); !slices.Equal(got, want) {
			t.Fatalf("got targets %v, want %v", got, want)
		}
		context := newCommandTestContext(t)
		err := revokeCommand(registry).Run(context, slash.Arguments{Text: "tool:fetch /tool:fetch 8080 tool:missing"})
		if err == nil || !strings.Contains(err.Error(), `no revocable target "tool:missing"`) {
			t.Fatalf("got failure %v", err)
		}
		if len(*paths) != 0 || len(*forwarded) != 0 || len(context.events) != 2 {
			t.Fatalf("paths or ports did not revoke: %+v, %+v, %+v", *paths, *forwarded, context.events)
		}
		if got := revocableSubjects(registry); !slices.Equal(got, []string{"tool:weather"}) {
			t.Fatalf("remaining targets: %v", got)
		}
		if context.success != "Revoked the approval for tool:fetch." {
			t.Fatalf("got confirmation %q", context.success)
		}
		time.Sleep(24 * time.Hour)
		if err := revokeCommand(registry).Run(context, slash.Arguments{Text: "tool:weather"}); err != nil {
			t.Fatal(err)
		}
		if len(broker.RememberedAnswers()) != 0 {
			t.Fatal("the session answer remained")
		}
	})
}

func TestGoldenRememberedAnswersAppearInGrantsAndLeaveWhenExpired(t *testing.T) {
	var output strings.Builder
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		rememberAnswerForTest(t, broker, "tool:fetch", false)
		rememberAnswerForTest(t, broker, "network:host", true)
		grants, _ := fixturePathGrants()
		registry := newRevocations(grants, Forwards{}, []revocation.Source{AnswerRevocations{Broker: broker}})
		context := newCommandTestContext(t)
		for _, stage := range []string{"active", "expired", "forgotten"} {
			switch stage {
			case "expired":
				time.Sleep(5 * time.Minute)
			case "forgotten":
				broker.Forget("network:host")
			}
			if err := grantsCommand(grants, Forwards{}, broker).Run(context, slash.Arguments{}); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&output, "=== %s ===\n", stage)
			fmt.Fprintln(&output, renderCommandFeedback(context, 80))
			targets := strings.Join(revocableSubjects(registry), ", ")
			if targets == "" {
				targets = "(none)"
			}
			fmt.Fprintf(&output, "revoke: %s\n", targets)
		}
	})
	assertGolden(t, "remembered-answers.txt", output.String())
}

func TestAnswerRevocationsUseQuestionKeysWithoutAnExtraPrefix(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := question.NewBroker()
		t.Cleanup(broker.Open())
		keys := []string{"network:host", "tool:commit", "tool:fetch", "tool:lookup"}
		for _, key := range keys {
			rememberAnswerForTest(t, broker, key, true)
		}
		registry := newRevocations(PathGrants{}, Forwards{}, []revocation.Source{AnswerRevocations{Broker: broker}})
		if targets := revocableSubjects(registry); !slices.Equal(targets, keys) {
			t.Fatalf("got targets %v, want %v", targets, keys)
		}
		for _, key := range keys {
			if _, err := registry.Revoke(key); err != nil {
				t.Fatal(err)
			}
		}
		if len(broker.RememberedAnswers()) != 0 {
			t.Fatal("revoking the targets did not forget their answers")
		}
	})
}
