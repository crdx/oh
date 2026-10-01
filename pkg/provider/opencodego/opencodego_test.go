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

	client, err := opencodego.New(url, "secret", "deepseek-v4-pro", "high", 128_000)
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

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "deepseek-v4-pro", "low", 64_000)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/chat/completions" {
		t.Errorf("got path %q", sent.path)
	}
	if sent.body["model"] != "deepseek-v4-pro" || sent.body["reasoning_effort"] != "low" ||
		sent.body["max_completion_tokens"] != float64(64_000) {
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

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "qwen3.8-max", "xhigh", 64_000)
	if err != nil {
		t.Fatal(err)
	}
	sendOnce(t, client)

	if sent.path != "/v1/messages" {
		t.Errorf("got path %q", sent.path)
	}
	if sent.body["model"] != "qwen3.8-max" || sent.body["max_tokens"] != float64(64_000) {
		t.Errorf("expected the model and output limit to be sent, got %v", sent.body)
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

	if sent.header.Get("Authorization") != "Bearer secret" ||
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

func TestAMessagesModelTakingNoEffortIsAskedToThink(t *testing.T) {
	server, sent := recordingServer(t, stoppedMessage)

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "minimax-m3", "", 64_000)
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

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "muse-spark-1.3-contributor", "", 64_000)
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

func TestAChatModelTakingNoEffortIsSentNoEffort(t *testing.T) {
	server, sent := recordingServer(t, "data: [DONE]\n\n")

	client, err := opencodego.New(server.URL+"/v1/chat/completions", "secret", "kimi-k2.7-code", "", 64_000)
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

	for modelID, want := range map[string]int{
		"deepseek-v4-pro":            chatcompletions.ToolsSize(tools),
		"muse-spark-1.3-contributor": responses.ToolsSize(tools),
		"qwen3.8-max":                messages.ToolsSize(tools),
	} {
		client, err := opencodego.New("http://somewhere/v1/chat/completions", "secret", modelID, "high", 64_000)
		if err != nil {
			t.Fatal(err)
		}

		if got := client.ToolsSize(tools); got != want {
			t.Errorf("%s: got %d, want %d", modelID, got, want)
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
		maxOutputTokens int
		want            string
	}{
		{"url", "", "secret", "deepseek-v4-pro", 128_000, "chat: URL is empty"},
		{"token", "http://somewhere", "", "deepseek-v4-pro", 128_000, "chat: Token is empty"},
		{"model", "http://somewhere", "secret", "", 128_000, "chat: Model is empty"},
		{"max tokens", "http://somewhere", "secret", "deepseek-v4-pro", 0, "chat: MaxOutputTokens is 0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := opencodego.New(test.url, test.token, test.model, "high", test.maxOutputTokens)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if client != nil {
				t.Errorf("expected no client, got %+v", client)
			}
		})
	}
}
