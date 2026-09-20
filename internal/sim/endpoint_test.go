package sim_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/sim"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/anthropic"
	"crdx.org/io/pkg/provider/codex"
	"crdx.org/io/pkg/provider/ollama"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/wire/openai/chatcompletions"
)

type params struct {
	City string `json:"city"`
}

func weather(callCount *int) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args params) (string, string) { return args.City, "" },
	).Plain(func(_ context.Context, args params) (string, error) {
		*callCount++
		return "raining in " + args.City, nil
	})
}

type speaker struct {
	name                 string
	format               string
	hasModelCapabilities bool
	connect              func(t *testing.T, address string) agent.Provider
}

func providers() []speaker {
	return []speaker{
		{
			name:                 "codex",
			format:               sim.Responses,
			hasModelCapabilities: true,
			connect: func(t *testing.T, address string) agent.Provider {
				t.Helper()

				client, err := codex.New(codex.Static("token", "account"), "fake", "high")
				if err != nil {
					t.Fatal(err)
				}
				client.URL = address

				return client
			},
		},
		{
			name:   "chat-completions",
			format: sim.Completions,
			connect: func(t *testing.T, address string) agent.Provider {
				t.Helper()

				client, err := chatcompletions.New(address, http.Header{"Authorization": {"Bearer token"}}, "fake", "high", 128_000)
				if err != nil {
					t.Fatal(err)
				}

				return client
			},
		},
		{
			name:                 "anthropic",
			format:               sim.Messages,
			hasModelCapabilities: true,
			connect: func(t *testing.T, address string) agent.Provider {
				t.Helper()

				client, err := anthropic.New(anthropic.Static("token"), "fake", "high", 128_000)
				if err != nil {
					t.Fatal(err)
				}
				client.URL = address

				return client
			},
		},
		{
			name:                 "ollama",
			format:               sim.Completions,
			hasModelCapabilities: true,
			connect: func(t *testing.T, address string) agent.Provider {
				t.Helper()

				client, err := ollama.New(address, "fake", "high", 128_000)
				if err != nil {
					t.Fatal(err)
				}

				return client
			},
		},
	}
}

func standUp(t *testing.T, scenario *sim.Scenario, provider speaker) (*sim.Endpoint, string) {
	t.Helper()

	endpoint := sim.New(scenario)
	server := httptest.NewServer(endpoint)

	t.Cleanup(server.Close)

	return endpoint, endpoint.Addresses(server.URL)[provider.format]
}

func newAgent(t *testing.T, provider speaker, address string, tools []tool.Tool) *agent.Agent {
	t.Helper()

	assistant := newPatientAgent(t, provider, address, tools)

	assistant.TakeRetryWaitsAtOnce()

	return assistant
}

func newPatientAgent(t *testing.T, provider speaker, address string, tools []tool.Tool) *agent.Agent {
	t.Helper()

	return agent.New("You are a helpful assistant", provider.connect(t, address), tools)
}

var conversation = &sim.Scenario{
	Model:  "fake",
	Strict: true,
	Turns: []sim.Turn{
		{
			Think: []string{"They want the weather."},
			Say:   "Let me look. ",
			Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"London"}`}},
		},
		{
			Say: "It is raining in London.",
		},
	},
}

func TestTheScenarioIsPlayedOneTurnPerRequest(t *testing.T) {
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			endpoint, address := standUp(t, conversation, provider)

			var callCount int

			assistant := newAgent(t, provider, address, []tool.Tool{weather(&callCount)})

			answer, err := assistant.Send(t.Context(), "what is the weather in London?")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if want := "Let me look. It is raining in London."; answer != want {
				t.Errorf("expected %q, got %q", want, answer)
			}

			if callCount != 1 {
				t.Errorf("expected the tool to run once, ran %d times", callCount)
			}

			askedRequests := endpoint.Requests()
			if len(askedRequests) != 2 {
				t.Fatalf("expected two requests, got %d", len(askedRequests))
			}

			if askedRequests[0].Turn != 0 || askedRequests[1].Turn != 1 {
				t.Errorf("expected the scenario to advance one turn per request, got %d then %d",
					askedRequests[0].Turn, askedRequests[1].Turn)
			}

			if !carries(askedRequests[1].Input, sim.CallOutput) {
				t.Errorf("expected the second request to answer the call, got %v",
					askedRequests[1].Input)
			}

			if len(askedRequests[0].Tools) != 1 || askedRequests[0].Tools[0] != "weather" {
				t.Errorf("expected the tool to be offered, got %v", askedRequests[0].Tools)
			}
		})
	}
}

func TestTheReasoningIsReportedWhateverTheApi(t *testing.T) {
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, conversation, provider)

			var callCount int

			assistant := newAgent(t, provider, address, []tool.Tool{weather(&callCount)})

			var thoughts []string

			for update, err := range assistant.Stream(t.Context(), "what is the weather in London?", nil) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if update.Event == nil {
					continue
				}

				if update.Event.Kind == agent.ModelReasoningEvent {
					thoughts = append(thoughts, update.Event.Text)
				}
			}

			if want := "They want the weather."; strings.Join(thoughts, "") != want {
				t.Errorf("expected %q, got %q", want, strings.Join(thoughts, ""))
			}
		})
	}
}

func TestATurnDroppedMidCallDoesNotSpoilTheNext(t *testing.T) {
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, conversation, provider)

			var callCount int

			assistant := newAgent(t, provider, address, []tool.Tool{weather(&callCount)})

			for update, err := range assistant.Stream(t.Context(), "what is the weather in London?", nil) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if update.Event == nil {
					continue
				}

				if update.Event.Kind == agent.ToolCallRequestEvent {
					break
				}
			}

			if callCount != 0 {
				t.Errorf("expected the tool not to have run, ran %d times", callCount)
			}

			if _, err := assistant.Send(t.Context(), "never mind, what about Paris?"); err != nil {
				t.Fatalf("the endpoint refused what the cancelled turn left behind: %v", err)
			}
		})
	}
}

func TestAScenarioThatRunsOutSaysSo(t *testing.T) {
	running := &sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Only this."}}}

	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, running, provider)

			assistant := newAgent(t, provider, address, nil)

			if _, err := assistant.Send(t.Context(), "first"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			answer, err := assistant.Send(t.Context(), "second")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(answer, "nothing more to say") {
				t.Errorf("expected the scenario to say it is over, got %q", answer)
			}
		})
	}
}

func TestAFailedTurnIsReported(t *testing.T) {
	failing := &sim.Scenario{
		Model: "fake",
		Loop:  true,
		Turns: []sim.Turn{{Fail: "the model is overloaded"}},
	}

	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, failing, provider)

			_, err := newAgent(t, provider, address, nil).Send(t.Context(), "hello")
			if err == nil {
				t.Fatal("expected the failure to be reported")
			}

			if err.Error() != "the model is overloaded" {
				t.Errorf("expected the endpoint's own words, got %q", err)
			}
		})
	}
}

func TestATurnCutShortIsReported(t *testing.T) {
	cut := &sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "It is raining ", Incomplete: true}}}

	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, cut, provider)

			if _, err := newAgent(t, provider, address, nil).Send(t.Context(), "hello"); err == nil {
				t.Error("expected a turn cut short to be reported")
			}
		})
	}
}

func TestATruncatedStreamIsRefused(t *testing.T) {
	stopped := &sim.Scenario{
		Model: "fake",
		Loop:  true,
		Turns: []sim.Turn{{Think: []string{"Half a thought."}, Say: "never sent", Truncate: true}},
	}

	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, stopped, provider)

			if _, err := newAgent(t, provider, address, nil).Send(t.Context(), "hello"); err == nil {
				t.Error("expected a stream that stops mid-turn to be refused")
			}
		})
	}
}

func TestATruncatedStreamIsAskedAgainAndFinishes(t *testing.T) {
	recovered := &sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Say: "It is raining ", Truncate: true},
			{Say: "in London."},
		},
	}

	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			endpoint, address := standUp(t, recovered, provider)

			answer, err := newAgent(t, provider, address, nil).Send(t.Context(), "hello")
			if err != nil {
				t.Fatalf("expected the turn to recover, got %v", err)
			}

			if answer != "It is raining in London." {
				t.Errorf("expected the answer to carry on where it stopped, got %q", answer)
			}

			if asked := len(endpoint.Requests()); asked != 2 {
				t.Errorf("expected the request to be made twice, got %d", asked)
			}
		})
	}
}

func TestARefusalIsWaitedOutForAsLongAsItAsked(t *testing.T) {
	const asked = 100 * time.Millisecond

	busy := &sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Status: http.StatusTooManyRequests, RetryAfter: sim.Duration{Duration: asked}},
			{Say: "It is raining in London."},
		},
	}

	provider := providers()[0]
	_, address := standUp(t, busy, provider)

	started := time.Now()

	answer, err := newPatientAgent(t, provider, address, nil).Send(t.Context(), "hello")
	if err != nil {
		t.Fatalf("expected the turn to recover, got %v", err)
	}

	if answer != "It is raining in London." {
		t.Errorf("expected the answer once the endpoint was ready, got %q", answer)
	}

	if waited := time.Since(started); waited < asked {
		t.Errorf("expected the refusal to be waited out, and it waited %s", waited)
	}
}

func TestARefusedStatusIsReported(t *testing.T) {
	refused := &sim.Scenario{
		Model: "fake",
		Loop:  true,
		Turns: []sim.Turn{{Status: http.StatusTooManyRequests}},
	}

	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, refused, provider)

			_, err := newAgent(t, provider, address, nil).Send(t.Context(), "hello")
			if err == nil || !strings.Contains(err.Error(), "having a moment") {
				t.Errorf("expected the refusal to be reported, got %v", err)
			}
		})
	}
}

func TestAModelListingAnswersEveryProvider(t *testing.T) {
	for _, provider := range providers() {
		t.Run(provider.name, func(t *testing.T) {
			_, address := standUp(t, conversation, provider)

			lister, canList := provider.connect(t, address).(agent.Lister)
			if !canList {
				t.Fatal("expected the provider to list its models")
			}

			models, err := lister.Models(t.Context())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(models) != 1 || models[0].ID != "fake" {
				t.Fatalf("expected the scenario's model, got %v", models)
			}

			hasModelCapabilities := len(models[0].EffortLevels) > 0
			if hasModelCapabilities != provider.hasModelCapabilities {
				t.Errorf("got effort capabilities %v, want reported %t", models[0].EffortLevels, provider.hasModelCapabilities)
			}
		})
	}
}

func responsesAddress(t *testing.T, scenario *sim.Scenario) string {
	t.Helper()

	for _, provider := range providers() {
		if provider.format == sim.Responses {
			_, address := standUp(t, scenario, provider)

			return address
		}
	}

	t.Fatal("nothing here speaks the Responses API")

	return ""
}

func post(t *testing.T, address string, body string) *http.Response {
	t.Helper()

	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, address, strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	return response
}

func TestACallWithNoOutputIsRefused(t *testing.T) {
	address := responsesAddress(t, conversation)

	body, err := json.Marshal(map[string]any{
		"model":            "fake",
		"stream":           true,
		"store":            false,
		"instructions":     "You are a helpful assistant",
		"prompt_cache_key": "abandoned",
		"input": []any{
			map[string]any{"role": "user", "content": "what is the weather in London?"},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_0",
				"name":      "weather",
				"arguments": `{"city":"London"}`,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	response := post(t, address, string(body))

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("expected the request to be refused, got %d", response.StatusCode)
	}

	var refusal struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(response.Body).Decode(&refusal); err != nil {
		t.Fatal(err)
	}

	if want := "No tool output found for function call call_0."; refusal.Error.Message != want {
		t.Errorf("expected %q, got %q", want, refusal.Error.Message)
	}
}

func TestARawErrorEventIsReportedAsJSON(t *testing.T) {
	raw := &sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{{
			ErrorEvent: `{"type":"error","error":{"code":"overloaded","retry":true}}`,
		}},
	}

	client, err := codex.New(codex.Static("token", "account"), "fake", "high")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = responsesAddress(t, raw)

	_, err = agent.New("You are a helpful assistant", client, nil).Send(t.Context(), "hello")
	want := `{
  "type": "error",
  "error": {
    "code": "overloaded",
    "retry": true
  }
}`
	if err == nil || err.Error() != want {
		t.Errorf("expected the raw error event as JSON, got %v", err)
	}
}

func TestAnAddressThatNoApiAnswersAtIsRefused(t *testing.T) {
	server := httptest.NewServer(sim.New(conversation))

	t.Cleanup(server.Close)

	response := post(t, server.URL+"/v1/somewhere", "{}")

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Errorf("expected an address nothing answers at to be refused, got %d", response.StatusCode)
	}
}

func carries(input []sim.Entry, kind string) bool {
	for _, entry := range input {
		if entry.Type == kind {
			return true
		}
	}

	return false
}
