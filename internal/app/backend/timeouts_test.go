package backend

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

func TestEveryProviderReadsLiveStreamTimeouts(t *testing.T) {
	for _, test := range []struct {
		provider string
		wire     agent.Wire
	}{
		{model.CodexProvider, agent.ResponsesWire},
		{model.AnthropicProvider, agent.MessagesWire},
		{model.OllamaProvider, agent.CompletionsWire},
		{model.OpencodeGoProvider, agent.CompletionsWire},
		{model.OpencodeGoProvider, agent.ResponsesWire},
		{model.OpencodeGoProvider, agent.MessagesWire},
		{model.OpencodeGoProvider, agent.GenerateContentWire},
	} {
		t.Run(test.provider+"/"+string(test.wire), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				<-request.Context().Done()
			}))
			t.Cleanup(server.Close)
			var timeouts atomic.Pointer[req.StreamTimeouts]
			timeouts.Store(&req.StreamTimeouts{Response: 25 * time.Millisecond, Idle: time.Second})
			connection, err := Connect(
				model.Choice{Provider: test.provider, ID: "fake", Wire: test.wire, MaxOutputTokens: 1024},
				model.Selection{Provider: test.provider, Model: "fake", Effort: "high"},
				EndpointSettings{OverrideURL: server.URL + "/v1/chat/completions", StreamTimeouts: func() req.StreamTimeouts { return *timeouts.Load() }},
			)
			if err != nil {
				t.Fatal(err)
			}
			connection.Configure("", nil)
			connection.AddUserMessage("hello")
			for _, response := range []time.Duration{25 * time.Millisecond, 50 * time.Millisecond} {
				timeouts.Store(&req.StreamTimeouts{Response: response, Idle: time.Second})
				_, err := connection.Send(t.Context(), func(agent.Output) bool { return true })
				var timeout net.Error
				if !errors.As(err, &timeout) || !timeout.Timeout() {
					t.Fatalf("response timeout %s returned %v", response, err)
				}
			}
		})
	}
}
