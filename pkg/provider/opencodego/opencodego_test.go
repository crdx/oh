package opencodego_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/opencodego"
)

func newClient(t *testing.T, url string) *opencodego.Client {
	t.Helper()

	client, err := opencodego.New(url, "secret", "deepseek-v4-pro", "high", 128_000)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func TestNewHandsBackAnAuthenticatedChatCompletionsClient(t *testing.T) {
	client, err := opencodego.New(
		"http://somewhere/v1/chat/completions",
		"secret",
		"deepseek-v4-pro",
		"low",
		64_000,
	)
	if err != nil {
		t.Fatal(err)
	}

	if client.URL != "http://somewhere/v1/chat/completions" || client.Token != "secret" ||
		client.Model != "deepseek-v4-pro" || client.Effort != "low" || client.MaxOutputTokens != 64_000 {
		t.Errorf("expected what was asked for to be held verbatim, got %+v", client)
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

func TestToolsSizeDelegatesToTheWireFormat(t *testing.T) {
	if size := opencodego.ToolsSize(nil); size != len("[]") {
		t.Errorf("got empty tool size %d", size)
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
