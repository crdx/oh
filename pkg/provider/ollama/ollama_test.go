package ollama_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/req"
	"crdx.org/io/internal/sim"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/ollama"
)

func TestNewBuildsOllamaAddresses(t *testing.T) {
	client, err := ollama.New("speeder:11434/", "qwen3.8:27b", "high", 32_768)
	if err != nil {
		t.Fatal(err)
	}

	if client.EndpointURL != "http://speeder:11434" {
		t.Errorf("got endpoint %q", client.EndpointURL)
	}
	if client.URL != "http://speeder:11434/v1/chat/completions" {
		t.Errorf("got conversation URL %q", client.URL)
	}
	if client.Model != "qwen3.8:27b" || client.Effort != "high" || client.MaxOutputTokens != 32_768 {
		t.Errorf("client lost its model settings: %+v", client.Client)
	}
}

func TestNewAcceptsACompleteConversationAddress(t *testing.T) {
	address := "http://somewhere/v1/chat/completions"
	client, err := ollama.New(address, "model", "none", 8_192)
	if err != nil {
		t.Fatal(err)
	}

	if client.URL != address || client.EndpointURL != "http://somewhere" {
		t.Errorf("got endpoint %q and conversation URL %q", client.EndpointURL, client.URL)
	}
}

func TestNewRefusesMissingSettings(t *testing.T) {
	tests := []struct {
		name            string
		endpointURL     string
		model           string
		maxOutputTokens int
		want            string
	}{
		{name: "endpoint", model: "model", maxOutputTokens: 8_192, want: "EndpointURL is empty"},
		{name: "model", endpointURL: ollama.EndpointURL, maxOutputTokens: 8_192, want: "Model is empty"},
		{name: "output limit", endpointURL: ollama.EndpointURL, model: "model", want: "MaxOutputTokens is 0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := ollama.New(test.endpointURL, test.model, "high", test.maxOutputTokens)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got client %+v and error %v", test.want, client, err)
			}
		})
	}
}

func TestModelsOffersInstalledToolModelsWithTheirCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/tags" {
			t.Errorf("got request for %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"models":[`+
			`{"name":"thinking","capabilities":["completion","tools","thinking"],"details":{"context_length":200000}},`+
			`{"name":"plain","capabilities":["completion","tools"],"details":{"context_length":16000}},`+
			`{"name":"tiny-context","capabilities":["completion","tools"],"details":{"context_length":3}},`+
			`{"name":"unknown-context","capabilities":["completion","tools"],"details":{}},`+
			`{"name":"no-tools","capabilities":["completion"],"details":{"context_length":32000}},`+
			`{"name":"embedding","capabilities":["embedding"],"details":{"context_length":32000}}`+
			`]}`)
	}))
	t.Cleanup(server.Close)

	client, err := ollama.New(server.URL, "model", "none", 8_192)
	if err != nil {
		t.Fatal(err)
	}
	observer := &countingObserver{}
	client.ObserveHTTP(observer)
	models, err := client.Models(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if observer.requests != 1 {
		t.Errorf("observed %d model requests", observer.requests)
	}

	want := []agent.Model{
		{ID: "plain", EffortLevels: []string{"none"}, ContextWindowTokens: 16_000, MaxOutputTokens: 4_000},
		{ID: "thinking", EffortLevels: []string{"none", "low", "medium", "high"}, ContextWindowTokens: 200_000, MaxOutputTokens: 32_768},
		{ID: "tiny-context", EffortLevels: []string{"none"}, ContextWindowTokens: 3, MaxOutputTokens: 1},
		{ID: "unknown-context", EffortLevels: []string{"none"}, ContextWindowTokens: 32_768, MaxOutputTokens: 8_192},
	}
	if !slices.EqualFunc(models, want, func(got agent.Model, expected agent.Model) bool {
		return got.ID == expected.ID && slices.Equal(got.EffortLevels, expected.EffortLevels) &&
			got.ContextWindowTokens == expected.ContextWindowTokens && got.MaxOutputTokens == expected.MaxOutputTokens
	}) {
		t.Errorf("got models %+v, want %+v", models, want)
	}
}

func TestModelsReportsARefusedListing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, `{"error":{"message":"listing refused"}}`, http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client, err := ollama.New(server.URL, "model", "none", 8_192)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Models(t.Context())

	refused, ok := errors.AsType[*req.StatusError](err)
	if !ok || refused.Status != http.StatusForbidden {
		t.Fatalf("got error %v", err)
	}
}

func TestToolsSizeDelegatesToChatCompletions(t *testing.T) {
	if size := ollama.ToolsSize(nil); size != len("[]") {
		t.Errorf("got empty tool size %d", size)
	}
}

func TestOllamaCompletesAnUnauthenticatedConversationThroughTheChatProtocol(t *testing.T) {
	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	var authorisation string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorisation = request.Header.Get("Authorization")
		endpoint.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)

	client, err := ollama.New(server.URL, "fake", "high", 8_192)
	if err != nil {
		t.Fatal(err)
	}
	observer := &countingObserver{}
	client.ObserveHTTP(observer)
	answer, err := agent.New("Be helpful.", client, nil).Send(t.Context(), "Hello?")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Hello." {
		t.Errorf("got answer %q", answer)
	}
	if authorisation != "" {
		t.Errorf("got unexpected authorisation %q", authorisation)
	}
	if observer.requests != 1 {
		t.Errorf("observed %d conversation requests", observer.requests)
	}
}

type countingObserver struct {
	requests int
}

func (self *countingObserver) Start(req.Request) req.ExchangeObserver {
	self.requests++

	return nil
}
