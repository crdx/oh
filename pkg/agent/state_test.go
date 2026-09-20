package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type stateProvider struct {
	items []json.RawMessage
}

func (*stateProvider) Configure(string, []tool.Definition)   {}
func (*stateProvider) AddUserMessage(string)                 {}
func (*stateProvider) AddToolResults([]agent.ToolCallResult) {}
func (*stateProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}
func (self *stateProvider) Dump() []json.RawMessage      { return slices.Clone(self.items) }
func (self *stateProvider) Load(items []json.RawMessage) { self.items = slices.Clone(items) }

func TestAgentCarriesProviderState(t *testing.T) {
	provider := &stateProvider{}
	assistant := agent.New("", provider, nil)
	want := []json.RawMessage{json.RawMessage(`{"type":"message"}`)}

	if err := assistant.Load(want); err != nil {
		t.Fatal(err)
	}
	got, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(got, want, func(a, b json.RawMessage) bool { return string(a) == string(b) }) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAgentRejectsStateThatIsNotAppendOnly(t *testing.T) {
	provider := &stateProvider{items: []json.RawMessage{json.RawMessage(`{"one":1}`)}}
	assistant := agent.New("", provider, nil)
	if _, err := assistant.Dump(); err != nil {
		t.Fatal(err)
	}

	provider.items[0] = json.RawMessage(`{"two":2}`)
	if _, err := assistant.Dump(); !errors.Is(err, agent.ErrStateReplaced) {
		t.Errorf("expected ErrStateReplaced, got %v", err)
	}
}

func TestAgentStateDoesNotAliasItsCallerOrProvider(t *testing.T) {
	original := json.RawMessage(`{"value":"original"}`)
	provider := &stateProvider{}
	assistant := agent.New("", provider, nil)

	if err := assistant.Load([]json.RawMessage{original}); err != nil {
		t.Fatal(err)
	}
	copy(original, `{"value":"changed!"}`)

	first, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(first[0]); got != `{"value":"original"}` {
		t.Fatalf("loaded state changed through its caller: %s", got)
	}

	copy(first[0], `{"value":"changed!"}`)
	second, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(second[0]); got != `{"value":"original"}` {
		t.Errorf("dumped state changed through its recipient: %s", got)
	}
}

func TestAgentDetectsProviderStateMutatedInPlace(t *testing.T) {
	provider := &stateProvider{items: []json.RawMessage{json.RawMessage(`{"value":"original"}`)}}
	assistant := agent.New("", provider, nil)

	if _, err := assistant.Dump(); err != nil {
		t.Fatal(err)
	}
	copy(provider.items[0], `{"value":"changed!"}`)

	if _, err := assistant.Dump(); !errors.Is(err, agent.ErrStateReplaced) {
		t.Errorf("expected ErrStateReplaced, got %v", err)
	}
}

func TestAgentReportsAProviderWithoutState(t *testing.T) {
	assistant := agent.New("", &callProvider{}, nil)
	if _, err := assistant.Dump(); !errors.Is(err, agent.ErrNoState) {
		t.Errorf("expected ErrNoState, got %v", err)
	}
	if err := assistant.Load(nil); !errors.Is(err, agent.ErrNoState) {
		t.Errorf("expected ErrNoState, got %v", err)
	}
}

func statefulTool(restored *json.RawMessage) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).State("test_state", func(state json.RawMessage) error {
		*restored = append((*restored)[:0], state...)
		return nil
	}).Run(func(context.Context, struct{}) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{
			Output: "done",
			State:  json.RawMessage(`{"answer":42}`),
		}, nil
	})
}

func TestASuccessfulCallEmitsItsDurableStateBeforeItsResult(t *testing.T) {
	provider := &callProvider{}
	var restored json.RawMessage
	assistant := agent.New("", provider, []tool.Tool{statefulTool(&restored)})
	var events []agent.Event

	for update, err := range assistant.Stream(t.Context(), "go", nil) {
		if err != nil {
			t.Fatal(err)
		}

		if update.Event == nil {
			continue
		}
		events = append(events, *update.Event)
	}

	var relevant []agent.Kind
	var state json.RawMessage
	var stateKey string
	for _, event := range events {
		if event.Kind == agent.ToolCallResultEvent || event.Kind == agent.StateChangeEvent {
			relevant = append(relevant, event.Kind)
		}
		if event.Kind == agent.StateChangeEvent {
			state = event.State
			stateKey = event.Name
		}
	}

	wantKinds := []agent.Kind{agent.StateChangeEvent, agent.ToolCallResultEvent, agent.StateChangeEvent, agent.ToolCallResultEvent}
	if !reflect.DeepEqual(relevant, wantKinds) {
		t.Errorf("got event kinds %v, want %v", relevant, wantKinds)
	}
	if stateKey != "test_state" || string(state) != `{"answer":42}` {
		t.Errorf("got %s state %s", stateKey, state)
	}
	if string(restored) != `{"answer":42}` {
		t.Errorf("got live state %s", restored)
	}
}

func TestAFailedCallDoesNotEmitDurableState(t *testing.T) {
	provider := &callProvider{}
	failedTool := tool.Implement(
		tool.Definition{Name: "noop", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Run(func(context.Context, struct{}) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{State: json.RawMessage(`{"answer":42}`)}, errors.New("failed")
	})
	assistant := agent.New("", provider, []tool.Tool{failedTool})

	for update, err := range assistant.Stream(t.Context(), "go", nil) {
		if err != nil {
			t.Fatal(err)
		}

		if update.Event == nil {
			continue
		}
		if update.Event.Kind == agent.StateChangeEvent {
			t.Error("a failed call emitted durable state")
		}
	}
}

func TestStateCanBeRestoredIntoANewAgent(t *testing.T) {
	provider := &callProvider{}
	var restored json.RawMessage
	assistant := agent.New("", provider, []tool.Tool{statefulTool(&restored)})
	events := []agent.Event{{
		Kind:  agent.StateChangeEvent,
		Name:  "test_state",
		State: json.RawMessage(`{"answer":42}`),
	}}

	if err := assistant.RestoreState(events); err != nil {
		t.Fatal(err)
	}
	if string(restored) != `{"answer":42}` {
		t.Errorf("got restored state %s", restored)
	}
}

func TestStateCanBeRestoredIntoADisabledTool(t *testing.T) {
	var restored json.RawMessage
	knownTool := statefulTool(&restored)
	assistant := agent.NewWithEnabledTools("", &callProvider{}, []tool.Tool{knownTool}, nil)
	events := []agent.Event{{
		Kind:  agent.StateChangeEvent,
		Name:  "test_state",
		State: json.RawMessage(`{"answer":42}`),
	}}

	if err := assistant.RestoreState(events); err != nil {
		t.Fatal(err)
	}
	if string(restored) != `{"answer":42}` {
		t.Errorf("got restored state %s", restored)
	}
}
