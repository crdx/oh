package opencodego_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/opencodego"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/anthropic/messages"
	"crdx.org/oh/pkg/wire/openai/chatcompletions"
	"crdx.org/oh/pkg/wire/openai/responses"
)

func newClient(t *testing.T, url string) *opencodego.Client {
	t.Helper()

	client, err := opencodego.New(url, "secret", "deepseek-v4-pro", agent.CompletionsWire, "high", 128_000)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

type sentRequest struct {
	path   string
	header http.Header
	body   map[string]any
}

func recordingServer(t *testing.T, reply string) (*httptest.Server, *sentRequest) {
	t.Helper()

	sent := &sentRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sent.path = request.URL.Path
		sent.header = request.Header.Clone()
		_ = json.NewDecoder(request.Body).Decode(&sent.body)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, reply)
	}))
	t.Cleanup(server.Close)

	return server, sent
}

func sendOnce(t *testing.T, client *opencodego.Client) {
	t.Helper()

	client.UseSession("0123456789ABCDEFGHIJKL")
	client.Configure("Be brief.", nil)
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}
}

func TestAChatModelIsAskedThroughChatCompletions(t *testing.T) {
	server, sent := recordingServer(t, "data: [DONE]\n\n")

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "deepseek-v4-pro", agent.CompletionsWire, "low", 16_000)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/chat/completions" {
		t.Errorf("got path %q", sent.path)
	}
	if sent.body["model"] != "deepseek-v4-pro" || sent.body["reasoning_effort"] != "low" ||
		sent.body["max_completion_tokens"] != float64(16_000) {
		t.Errorf("expected what was asked for to be sent verbatim, got %v", sent.body)
	}
}

const completedResponse = "data: {\"type\":\"response.completed\",\"response\":{\"usage\":" +
	"{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"

func TestAResponsesModelIsAskedThroughResponsesBesideChatCompletions(t *testing.T) {
	server, sent := recordingServer(t, completedResponse)

	client, err := opencodego.New(
		server.URL+"/v1/chat/completions",
		"secret",
		"muse-spark-1.3-contributor",
		agent.ResponsesWire,
		"high",
		64_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/responses" {
		t.Errorf("got path %q", sent.path)
	}
	if sent.body["model"] != "muse-spark-1.3-contributor" || sent.body["instructions"] != "Be brief." {
		t.Errorf("expected the model and instructions to be sent, got %v", sent.body)
	}
	if reasoning, _ := sent.body["reasoning"].(map[string]any); reasoning["effort"] != "high" {
		t.Errorf("expected the effort to be sent as a reasoning effort, got %v", sent.body["reasoning"])
	}
	if sent.header.Get("Authorization") != "Bearer secret" ||
		sent.header.Get("X-Opencode-Session") != "0123456789ABCDEFGHIJKL" {
		t.Errorf("expected an authorised, scoped request, got %v", sent.header)
	}
	for _, name := range []string{"Chatgpt-Account-Id", "Originator", "X-Codex-Routing-Hint"} {
		if value := sent.header.Get(name); value != "" {
			t.Errorf("expected no %s header, got %q", name, value)
		}
	}
}

type weatherArguments struct {
	City string `json:"city"`
}

func weather() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(arguments weatherArguments) tool.CallRendering {
			return tool.CallRendering{Subject: arguments.City}
		},
	).Plain(func(context.Context, weatherArguments) (string, error) { return "raining", nil })
}

const stoppedMessage = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":" +
	"{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}," +
	"\"usage\":{\"output_tokens\":1}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func TestAMessagesModelIsAskedThroughMessagesWithoutClaudeCodesClothes(t *testing.T) {
	server, sent := recordingServer(t, stoppedMessage)

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "qwen3.8-max", agent.MessagesWire, "xhigh", 64_000)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/messages" {
		t.Errorf("got path %q", sent.path)
	}
	if sent.body["model"] != "qwen3.8-max" || sent.body["max_tokens"] != float64(32_000) {
		t.Errorf("expected the model and the capped output limit to be sent, got %v", sent.body)
	}
	if output, _ := sent.body["output_config"].(map[string]any); output["effort"] != "xhigh" {
		t.Errorf("expected the effort to be sent as an output effort, got %v", sent.body["output_config"])
	}
	if thinking, isSent := sent.body["thinking"]; isSent {
		t.Errorf("expected no thinking setting, got %v", thinking)
	}

	system, _ := sent.body["system"].([]any)
	if len(system) != 1 || !isTextBlock(system[0], "Be brief.") {
		t.Errorf("expected the instructions alone as the system prompt, got %v", system)
	}

	if sent.header.Get("X-Api-Key") != "secret" ||
		sent.header.Get("X-Opencode-Session") != "0123456789ABCDEFGHIJKL" ||
		sent.header.Get("Anthropic-Version") != messages.Version {
		t.Errorf("expected an authorised, scoped, versioned request, got %v", sent.header)
	}
	if want := fmt.Sprintf("oh (%s; %s)", runtime.GOOS, runtime.GOARCH); sent.header.Get("User-Agent") != want {
		t.Errorf("got user agent %q, want %q", sent.header.Get("User-Agent"), want)
	}
	for _, name := range []string{"Anthropic-Beta", "Anthropic-Dangerous-Direct-Browser-Access", "X-App"} {
		if value := sent.header.Get(name); value != "" {
			t.Errorf("expected no %s header, got %q", name, value)
		}
	}
}

func TestEachWireCarriesTheKeyWhereItsEndpointReadsIt(t *testing.T) {
	tests := []struct {
		wire       agent.Wire
		response   string
		keyName    string
		keyValue   string
		absentName string
	}{
		{agent.CompletionsWire, "data: [DONE]\n\n", "Authorization", "Bearer secret", "X-Api-Key"},
		{agent.ResponsesWire, completedResponse, "Authorization", "Bearer secret", "X-Api-Key"},
		{agent.MessagesWire, stoppedMessage, "X-Api-Key", "secret", "Authorization"},
	}

	for _, test := range tests {
		t.Run(string(test.wire), func(t *testing.T) {
			server, sent := recordingServer(t, test.response)

			client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "model", test.wire, "high", 64_000)
			if err != nil {
				t.Fatal(err)
			}
			sendOnce(t, client)

			if got := sent.header.Get(test.keyName); got != test.keyValue {
				t.Errorf("expected %s of %q, got %q", test.keyName, test.keyValue, got)
			}
			if got := sent.header.Get(test.absentName); got != "" {
				t.Errorf("expected no %s header, got %q", test.absentName, got)
			}
		})
	}
}

func TestAMessagesModelTakingNoEffortIsAskedToThink(t *testing.T) {
	server, sent := recordingServer(t, stoppedMessage)

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "minimax-m3", agent.MessagesWire, "", 64_000)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/messages" {
		t.Errorf("got path %q", sent.path)
	}
	if thinking, _ := sent.body["thinking"].(map[string]any); len(thinking) != 1 || thinking["type"] != "enabled" {
		t.Errorf("expected thinking to be switched on and nothing more, got %v", sent.body["thinking"])
	}
	if output, isSent := sent.body["output_config"]; isSent {
		t.Errorf("expected no output effort, got %v", output)
	}
}

func TestAResponsesModelTakingNoEffortIsSentNoEffort(t *testing.T) {
	server, sent := recordingServer(t, completedResponse)

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "muse-spark-1.3-contributor", agent.ResponsesWire, "", 64_000)
	if err != nil {
		t.Fatalf("expected a model taking no effort to connect, got %v", err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/responses" {
		t.Errorf("got path %q", sent.path)
	}
	reasoning, _ := sent.body["reasoning"].(map[string]any)
	if _, isSent := reasoning["effort"]; isSent || reasoning["summary"] != "auto" {
		t.Errorf("expected a reasoning summary and no effort, got %v", sent.body["reasoning"])
	}
}

func TestEveryWireIsSentTheOutputLimitCappedAsOpencodeCapsIt(t *testing.T) {
	tests := []struct {
		model           string
		wire            agent.Wire
		response        string
		maxOutputTokens int
		field           string
		want            float64
	}{
		{"muse-spark-1.3-contributor", agent.ResponsesWire, completedResponse, 131_072, "max_output_tokens", 32_000},
		{"muse-spark-1.3-contributor", agent.ResponsesWire, completedResponse, 16_000, "max_output_tokens", 16_000},
		{"qwen3.8-max", agent.MessagesWire, stoppedMessage, 131_072, "max_tokens", 32_000},
		{"qwen3.8-max", agent.MessagesWire, stoppedMessage, 16_000, "max_tokens", 16_000},
		{"kimi-k2.7-code", agent.CompletionsWire, "data: [DONE]\n\n", 131_072, "max_completion_tokens", 32_000},
		{"kimi-k2.7-code", agent.CompletionsWire, "data: [DONE]\n\n", 16_000, "max_completion_tokens", 16_000},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.model, test.maxOutputTokens), func(t *testing.T) {
			server, sent := recordingServer(t, test.response)

			client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", test.model, test.wire, "", test.maxOutputTokens)
			if err != nil {
				t.Fatal(err)
			}
			sendOnce(t, client)

			if sent.body[test.field] != test.want {
				t.Errorf("expected %s of %v, got %v", test.field, test.want, sent.body[test.field])
			}
		})
	}
}

func TestAChatModelTakingNoEffortIsSentNoEffort(t *testing.T) {
	server, sent := recordingServer(t, "data: [DONE]\n\n")

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "kimi-k2.7-code", agent.CompletionsWire, "", 64_000)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/chat/completions" {
		t.Errorf("got path %q", sent.path)
	}
	if effort, isSent := sent.body["reasoning_effort"]; isSent {
		t.Errorf("expected no reasoning effort, got %v", effort)
	}
}

func isTextBlock(block any, text string) bool {
	fields, isObject := block.(map[string]any)

	return isObject && fields["text"] == text
}

func TestToolsAreMeasuredInTheWireFormatTheModelIsAskedThrough(t *testing.T) {
	tools := []tool.Tool{weather()}

	for wire, want := range map[agent.Wire]int{
		agent.CompletionsWire: chatcompletions.ToolsSize(tools),
		agent.ResponsesWire:   responses.ToolsSize(tools),
		agent.MessagesWire:    messages.ToolsSize(tools),
	} {
		client, err := opencodego.New("http://somewhere/v1/chat/completions", "secret", "model", wire, "high", 64_000)
		if err != nil {
			t.Fatal(err)
		}

		if got := client.ToolsSize(tools); got != want {
			t.Errorf("%s: got %d, want %d", wire, got, want)
		}
	}

	if chatcompletions.ToolsSize(tools) == responses.ToolsSize(tools) {
		t.Error("expected the two wire formats to measure a tool differently")
	}
}

func TestConversationsAreIdentifiedAuthorisedScopedAndObserved(t *testing.T) {
	var authorisation string
	var session string
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorisation = request.Header.Get("Authorization")
		session = request.Header.Get("X-Opencode-Session")
		userAgent = request.Header.Get("User-Agent")
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL)
	observer := &countingObserver{}
	client.UseSession("0123456789ABCDEFGHIJKL")
	client.ObserveHTTP(observer)
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	if authorisation != "Bearer secret" {
		t.Errorf("got authorisation %q", authorisation)
	}
	if session != "0123456789ABCDEFGHIJKL" {
		t.Errorf("got session %q", session)
	}
	if want := fmt.Sprintf("oh (%s; %s)", runtime.GOOS, runtime.GOARCH); userAgent != want {
		t.Errorf("got user agent %q, want %q", userAgent, want)
	}
	if observer.requests != 1 {
		t.Errorf("observed %d requests", observer.requests)
	}
}

func TestNewPreservesSettingValidation(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		token           string
		model           string
		wire            agent.Wire
		maxOutputTokens int
		want            string
	}{
		{"url", "", "secret", "deepseek-v4-pro", agent.CompletionsWire, 128_000, "chat: URL is empty"},
		{"token", "http://somewhere", "", "deepseek-v4-pro", agent.CompletionsWire, 128_000, "chat: Token is empty"},
		{"model", "http://somewhere", "secret", "", agent.CompletionsWire, 128_000, "chat: Model is empty"},
		{"max tokens", "http://somewhere", "secret", "deepseek-v4-pro", agent.CompletionsWire, 0, "chat: MaxOutputTokens is 0"},
		{"responses max tokens", "http://somewhere", "secret", "muse-spark-1.3-contributor", agent.ResponsesWire, 0, "responses: MaxOutputTokens is 0"},
		{"messages max tokens", "http://somewhere", "secret", "qwen3.8-max", agent.MessagesWire, 0, "anthropic: MaxOutputTokens is 0"},
		{"no wire", "http://somewhere", "secret", "claude-haiku-5-5", "", 128_000, "no wire protocol is known for claude-haiku-5-5"},
		{"unknown wire", "http://somewhere", "secret", "gemini-9", "generative-language", 128_000, "no wire protocol is known for gemini-9"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := opencodego.New(test.url, test.token, test.model, test.wire, "high", test.maxOutputTokens)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if client != nil {
				t.Errorf("expected no client, got %+v", client)
			}
		})
	}
}
