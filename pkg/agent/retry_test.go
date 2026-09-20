package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type wireDiedError struct {
	wait time.Duration
}

func (wireDiedError) Error() string                  { return "the stream ended before the response did" }
func (wireDiedError) Retriable() bool                { return true }
func (self wireDiedError) RetryAfter() time.Duration { return self.wait }

type flatRefusalError struct{}

func (flatRefusalError) Error() string             { return "that request was no good" }
func (flatRefusalError) Retriable() bool           { return false }
func (flatRefusalError) RetryAfter() time.Duration { return 0 }

type failingProvider struct {
	failures int
	err      error

	sent int
}

func (self *failingProvider) Configure(string, []tool.Definition)   {}
func (self *failingProvider) AddUserMessage(string)                 {}
func (self *failingProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *failingProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent <= self.failures {
		yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "half an ans"})

		return agent.Reply{}, self.err
	}

	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "wer"})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})

	return agent.Reply{}, nil
}

func collect(t *testing.T, assistant *agent.Agent) ([]agent.Event, error) {
	t.Helper()

	var seen []agent.Event
	var failure error

	synctest.Test(t, func(t *testing.T) {
		for update, err := range assistant.Stream(t.Context(), "go", nil) {
			if err != nil {
				failure = err

				break
			}

			if update.Event != nil {
				seen = append(seen, *update.Event)
			}
		}
	})

	return seen, failure
}

func kinds(events []agent.Event) []agent.Kind {
	seen := make([]agent.Kind, len(events))
	for i, event := range events {
		seen[i] = event.Kind
	}

	return seen
}

func TestAFailedRequestThatIsWorthRepeatingIsMadeAgain(t *testing.T) {
	provider := &failingProvider{failures: 1, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	events, err := collect(t, assistant)
	if err != nil {
		t.Fatalf("expected the turn to recover, got %v", err)
	}

	if provider.sent != 2 {
		t.Errorf("expected the request to be made twice, got %d", provider.sent)
	}

	var said strings.Builder

	for _, event := range events {
		if event.Kind == agent.ModelMessageEvent {
			said.WriteString(event.Text)
		}
	}

	if said.String() != "half an answer" {
		t.Errorf("expected the answer to carry on where it stopped, got %q", said.String())
	}
}

func TestAFailedRequestSaysSoBeforeItIsMadeAgain(t *testing.T) {
	provider := &failingProvider{failures: 1, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	events, err := collect(t, assistant)
	if err != nil {
		t.Fatal(err)
	}

	var notices []agent.Event
	for _, event := range events {
		if event.Kind == agent.RetryingEvent {
			notices = append(notices, event)
		}
	}

	if len(notices) != 1 {
		t.Fatalf("expected one retry to be reported, got %v", kinds(events))
	}

	switch {
	case notices[0].Attempt != 1:
		t.Errorf("expected the failed attempt to be numbered, got %d", notices[0].Attempt)
	case notices[0].Took <= 0:
		t.Errorf("expected the wait to be reported, got %s", notices[0].Took)
	case agent.FailureText(notices[0]) != "the stream ended before the response did":
		t.Errorf("expected what stopped it as the provider put it, got %+v", notices[0].Failure)
	}
}

func TestARequestThatKeepsFailingGivesUpAndReportsTheLastFailure(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	_, err := collect(t, assistant)

	if _, ok := errors.AsType[wireDiedError](err); !ok {
		t.Fatalf("expected the failure to be reported, got %v", err)
	}
}

func TestAFailureNotWorthRepeatingIsReportedStraightAway(t *testing.T) {
	tests := map[string]error{
		"the endpoint refused the request":  flatRefusalError{},
		"nothing said whether to try again": errors.New("something went wrong"),
	}

	for name, failure := range tests {
		t.Run(name, func(t *testing.T) {
			provider := &failingProvider{failures: 100, err: failure}
			assistant := agent.New("", provider, nil)

			if _, err := collect(t, assistant); err == nil {
				t.Fatal("expected the failure to be reported")
			}

			if provider.sent != 1 {
				t.Errorf("expected one attempt and no more, got %d", provider.sent)
			}
		})
	}
}

func TestAnEndpointAskingToBeLeftAloneForTooLongIsNotWaitedFor(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{wait: time.Hour}}
	assistant := agent.New("", provider, nil)

	if _, err := collect(t, assistant); err == nil {
		t.Fatal("expected the failure to be reported")
	}

	if provider.sent != 1 {
		t.Errorf("expected one attempt and no more, got %d", provider.sent)
	}
}

func TestAnEndpointCanLengthenButNotShortenTheRetryWait(t *testing.T) {
	tests := map[string]struct {
		asked time.Duration
		want  time.Duration
	}{
		"shorter": {asked: 40 * time.Millisecond, want: time.Second},
		"longer":  {asked: 2 * time.Second, want: 2 * time.Second},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			provider := &failingProvider{failures: 1, err: wireDiedError{wait: test.asked}}
			assistant := agent.New("", provider, nil)

			events, err := collect(t, assistant)
			if err != nil {
				t.Fatal(err)
			}

			for _, event := range events {
				if event.Kind == agent.RetryingEvent && event.Took != test.want {
					t.Errorf("retry wait = %s, want %s", event.Took, test.want)
				}
			}
		})
	}
}

func TestStoppingTheTurnDuringAWaitEndsItThereAndThen(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	started := time.Now()

	for update, err := range assistant.Stream(ctx, "go", nil) {
		if err != nil {
			break
		}

		if update.Event != nil && update.Event.Kind == agent.RetryingEvent {
			cancel()
		}
	}

	if provider.sent != 1 {
		t.Errorf("expected the wait to be given up on, got %d attempts", provider.sent)
	}

	if waited := time.Since(started); waited > time.Second {
		t.Errorf("expected the wait to end when the turn did, and it took %s", waited)
	}
}

func TestNothingIsRepeatedOnceTheCallerHasStoppedListening(t *testing.T) {
	provider := &failingProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	for update := range assistant.Stream(t.Context(), "go", nil) {
		if update.Delta != nil && update.Delta.Kind == agent.ModelMessageEvent {
			break
		}
	}

	if provider.sent != 1 {
		t.Errorf("expected an abandoned turn not to be retried, got %d attempts", provider.sent)
	}
}

type resumedError struct{}

func (resumedError) Error() string             { return "the tool call was malformed" }
func (resumedError) Retriable() bool           { return true }
func (resumedError) RetryAfter() time.Duration { return 0 }
func (resumedError) Resumable() bool           { return true }

type failingStateProvider struct {
	failures int
	err      error

	sent  int
	items []json.RawMessage
}

func (*failingStateProvider) Configure(string, []tool.Definition)   {}
func (*failingStateProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *failingStateProvider) AddUserMessage(message string) {
	self.items = append(self.items, json.RawMessage(`"asked: `+message+`"`))
}

func (self *failingStateProvider) Dump() []json.RawMessage      { return slices.Clone(self.items) }
func (self *failingStateProvider) Load(items []json.RawMessage) { self.items = slices.Clone(items) }

func (self *failingStateProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent <= self.failures {
		self.items = append(self.items, json.RawMessage(`"abandoned attempt "`))
		yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "half an ans"})

		return agent.Reply{}, self.err
	}

	self.items = append(self.items, json.RawMessage(`"answered"`))
	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "wer"})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})

	return agent.Reply{}, nil
}

func held(provider *failingStateProvider) string {
	var all strings.Builder
	for _, item := range provider.items {
		all.Write(item)
	}

	return all.String()
}

func TestAnAbandonedAttemptLeavesNothingBehindForTheOneThatReplacesIt(t *testing.T) {
	provider := &failingStateProvider{failures: 2, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	if _, err := collect(t, assistant); err != nil {
		t.Fatalf("expected the turn to recover, got %v", err)
	}

	if want := `"asked: go""answered"`; held(provider) != want {
		t.Errorf("expected the conversation to hold %s, got %s", want, held(provider))
	}
}

func TestAFailureTheNextAttemptCarriesOnFromIsNotRewound(t *testing.T) {
	provider := &failingStateProvider{failures: 1, err: resumedError{}}
	assistant := agent.New("", provider, nil)

	if _, err := collect(t, assistant); err != nil {
		t.Fatalf("expected the turn to recover, got %v", err)
	}

	if want := `"asked: go""abandoned attempt ""answered"`; held(provider) != want {
		t.Errorf("expected the conversation to hold %s, got %s", want, held(provider))
	}
}

func TestTheLastAttemptOfATurnThatKeepsFailingKeepsWhatItSaid(t *testing.T) {
	provider := &failingStateProvider{failures: 100, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)

	if _, err := collect(t, assistant); err == nil {
		t.Fatal("expected the turn to fail")
	}

	if want := `"asked: go""abandoned attempt "`; held(provider) != want {
		t.Errorf("expected the conversation to hold %s, got %s", want, held(provider))
	}
}

func TestARequestThatKeepsFailingIsGivenUpOnWithinTheBudget(t *testing.T) {
	provider := &failingProvider{failures: 1000, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)
	assistant.TakeRetryWaitsAtOnce()

	events, err := collect(t, assistant)
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}

	const wantAttempts = 19
	if provider.sent != wantAttempts {
		t.Errorf("attempts = %d, want %d before the budget runs out", provider.sent, wantAttempts)
	}

	var waited time.Duration
	for _, event := range events {
		if event.Kind == agent.RetryingEvent {
			waited += event.Took
		}
	}

	if waited > 15*time.Minute {
		t.Errorf("expected no more than fifteen minutes of waiting, got %s", waited)
	}

	if waited < 10*time.Minute {
		t.Errorf("expected the budget to be spent before giving up, got %s", waited)
	}
}

func TestRequestsAreRetriedOnTheFixedCadence(t *testing.T) {
	provider := &failingProvider{failures: 6, err: wireDiedError{}}
	assistant := agent.New("", provider, nil)
	assistant.TakeRetryWaitsAtOnce()

	events, err := collect(t, assistant)
	if err != nil {
		t.Fatal(err)
	}

	var waits []time.Duration
	for _, event := range events {
		if event.Kind == agent.RetryingEvent {
			waits = append(waits, event.Took)
		}
	}

	want := []time.Duration{
		time.Second,
		5 * time.Second,
		10 * time.Second,
		30 * time.Second,
		time.Minute,
		time.Minute,
	}
	if !slices.Equal(waits, want) {
		t.Errorf("retry waits = %v, want %v", waits, want)
	}
}
