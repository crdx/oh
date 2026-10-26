package sim_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/wire/anthropic/messages"
	"crdx.org/oh/pkg/wire/openai/responses"
)

func TestEndpointCompressionRequiresKnownRequestDecoding(t *testing.T) {
	for _, test := range []struct {
		address  string
		dialect  string
		encoding string
	}{
		{"https://api.openai.com/v1/responses", sim.Responses, "zstd"},
		{responses.Endpoint, sim.Responses, "zstd"},
		{"https://gateway.test/v1/responses", sim.Responses, ""},
		{"https://api.openai.com.gateway.test/v1/responses", sim.Responses, ""},
		{"http://api.openai.com/v1/responses", sim.Responses, ""},
		{"https://api.openai.com/other/responses", sim.Responses, ""},
		{messages.Endpoint, sim.Messages, "gzip"},
		{"https://gateway.test/v1/messages", sim.Messages, ""},
		{"https://api.anthropic.com.gateway.test/v1/messages", sim.Messages, ""},
		{"http://api.anthropic.com/v1/messages", sim.Messages, ""},
		{"https://api.anthropic.com/other/messages", sim.Messages, ""},
	} {
		t.Run(test.address, func(t *testing.T) {
			endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Done."}}})
			encodings := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				encodings <- request.Header.Get("Content-Encoding")
				endpoint.ServeHTTP(writer, request)
			}))
			t.Cleanup(server.Close)
			address := endpoint.Addresses(server.URL)[test.dialect]
			var client agent.Provider
			switch test.dialect {
			case sim.Responses:
				conversation, err := responses.NewAt(test.address, nil, "fake", "high", 128_000)
				if err != nil {
					t.Fatal(err)
				}
				conversation.URL = address
				client = conversation
			case sim.Messages:
				conversation, err := messages.NewAt(test.address, nil, "fake", "high", 128_000)
				if err != nil {
					t.Fatal(err)
				}
				conversation.URL = address
				client = conversation
			}
			var text strings.Builder
			for i := range 300 {
				fmt.Fprintf(&text, "Conversation entry %d remains unchanged.\n", i)
			}
			client.Configure("Be brief.", nil)
			client.AddUserMessage(text.String())
			if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
				t.Fatal(err)
			}
			if len(encodings) != 1 {
				t.Errorf("got %d requests, want one", len(encodings))
			}
			if got := <-encodings; got != test.encoding {
				t.Errorf("got encoding %q, want %q", got, test.encoding)
			}
		})
	}
}
