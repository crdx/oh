package chatcompletions_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/wire/openai/chatcompletions"
)

func scriptedServer(t *testing.T, bodies *[]string, payloads ...string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			*bodies = append(*bodies, string(body))

			writer.Header().Set("Content-Type", "text/event-stream")
			for _, payload := range payloads {
				_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
			}
		},
	))

	t.Cleanup(server.Close)

	return server
}

func newClient(t *testing.T, url string) *chatcompletions.Client {
	t.Helper()

	client, err := chatcompletions.New(url, bearerHeader(), "deepseek-v4-pro", "high", 128_000)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func bearerHeader() http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer secret")
	return header
}

func TestNewHandsBackAClientHoldingWhatItWasAsked(t *testing.T) {
	client, err := chatcompletions.New("http://somewhere/v1/chat/completions", bearerHeader(), "deepseek-v4-pro", "low", 64_000)
	if err != nil {
		t.Fatal(err)
	}

	if client.URL != "http://somewhere/v1/chat/completions" || client.Model != "deepseek-v4-pro" ||
		client.Effort != "low" || client.MaxOutputTokens != 64_000 {
		t.Errorf("expected what was asked for to be held verbatim, got %+v", client)
	}
}

func TestRequestHeadersAreOptionalAndOwnedByTheClient(t *testing.T) {
	for _, test := range []struct {
		name              string
		header            http.Header
		mutate            bool
		wantAuthorisation string
	}{
		{name: "no provider headers"},
		{name: "provider headers", header: bearerHeader(), mutate: true, wantAuthorisation: "Bearer secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var authorisation string
			var accepted string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				authorisation = request.Header.Get("Authorization")
				accepted = request.Header.Get("Accept")
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)

			client, err := chatcompletions.New(server.URL, test.header, "model", "none", 8_192)
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate {
				test.header.Set("Authorization", "Bearer changed")
			}
			client.AddUserMessage("hello")
			if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
				t.Fatal(err)
			}

			if authorisation != test.wantAuthorisation {
				t.Errorf("got authorisation %q, want %q", authorisation, test.wantAuthorisation)
			}
			if accepted != "text/event-stream" {
				t.Errorf("got accept %q", accepted)
			}
		})
	}
}

func TestASettingLeftOutIsRefusedRatherThanSubstituted(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		model           string
		maxOutputTokens int
		want            string
	}{
		{"url", "", "deepseek-v4-pro", 128_000, "chat: URL is empty"},
		{"model", "http://somewhere", "", 128_000, "chat: Model is empty"},
		{"max tokens", "http://somewhere", "deepseek-v4-pro", 0, "chat: MaxOutputTokens is 0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := chatcompletions.New(test.url, bearerHeader(), test.model, "high", test.maxOutputTokens)

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}

			if client != nil {
				t.Errorf("expected no client to be handed back, got %+v", client)
			}
		})
	}
}

func TestDumpAndLoadOwnTheirHistorySlices(t *testing.T) {
	client := newClient(t, "http://somewhere")
	loaded := []json.RawMessage{json.RawMessage(`{"role":"user","content":"original"}`)}
	client.Load(loaded)
	loaded[0] = json.RawMessage(`{"role":"user","content":"changed"}`)

	dumped := client.Dump()
	if got := string(dumped[0]); !strings.Contains(got, "original") {
		t.Fatalf("loaded history changed through its caller: %s", got)
	}

	dumped[0] = json.RawMessage(`{"role":"user","content":"changed"}`)
	if got := string(client.Dump()[0]); !strings.Contains(got, "original") {
		t.Errorf("dumped history changed through its recipient: %s", got)
	}
}

func TestPartialImagesAreNotSent(t *testing.T) {
	tests := map[string]tool.Image{
		"media type only": {MediaType: "image/png"},
		"data only":       {Data: []byte{1, 2, 3}},
	}

	for name, image := range tests {
		t.Run(name, func(t *testing.T) {
			client := newClient(t, "http://somewhere")
			client.AddToolResults([]agent.ToolCallResult{{ID: "call-1", Output: "text", Image: image}})

			if got := client.Dump(); len(got) != 1 {
				t.Errorf("partial image produced an attachment message: %s", got)
			}
		})
	}
}

func TestAnOversizedImageIsScaledBeforeItIsSent(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies, `{"choices":[{"delta":{"content":"seen"}}]}`, "[DONE]")

	client := newClient(t, server.URL)
	client.AddToolResults([]agent.ToolCallResult{{ID: "call-1", Output: "text", Image: oversizedPNG(t)}})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	requireBoundedImage(t, bodies[0], "base64,")
}

func TestAnOversizedImageInStoredHistoryIsScaledBeforeItIsSent(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies, `{"choices":[{"delta":{"content":"seen"}}]}`, "[DONE]")

	client := newClient(t, server.URL)
	client.Load([]json.RawMessage{storedImageItem(t)})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	requireBoundedImage(t, bodies[0], "base64,")
}

func TestStoredHistoryIsHandedBackExactlyAsItWasLoaded(t *testing.T) {
	client := newClient(t, "http://somewhere")
	stored := []json.RawMessage{storedImageItem(t)}

	client.Load(stored)

	dumped := client.Dump()
	if len(dumped) != 1 || !bytes.Equal(dumped[0], stored[0]) {
		t.Errorf("stored history was rewritten, so an append-only recorder would refuse it:\n%d bytes became %d",
			len(stored[0]), len(dumped[0]))
	}
}

func storedImageItem(t *testing.T) json.RawMessage {
	t.Helper()

	return json.RawMessage(
		`{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` +
			base64.StdEncoding.EncodeToString(oversizedPNG(t).Data) + `"}}]}`,
	)
}

func requireBoundedImage(t *testing.T, body string, marker string) {
	t.Helper()

	data := sentImage(t, body, marker)

	width, height, isMeasured := imageutil.Dimensions(data)
	if !isMeasured || width > imageutil.MaxEdge || height > imageutil.MaxEdge {
		t.Errorf("expected an image within %d pixels, got %dx%d", imageutil.MaxEdge, width, height)
	}
}

func oversizedPNG(t *testing.T) tool.Image {
	t.Helper()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2400, 1200))); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	return tool.Image{MediaType: "image/png", Data: encoded.Bytes()}
}

func sentImage(t *testing.T, dumped string, marker string) []byte {
	t.Helper()

	_, after, found := strings.Cut(dumped, marker)
	if !found {
		t.Fatalf("no image was sent: %s", dumped)
	}

	encoded, _, _ := strings.Cut(after, `"`)

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return data
}

func TestAnImageReturnedByAToolFollowsItInAMessageOfItsOwn(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"content":"seen"}}]}`, "[DONE]")

	client := newClient(t, server.URL)
	client.AddToolResults([]agent.ToolCallResult{{
		ID:     "call-1",
		Output: "picture.png",
		Image:  tool.Image{MediaType: "image/png", Data: []byte{1, 2, 3}},
	}})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &request); err != nil {
		t.Fatal(err)
	}

	if len(request.Messages) != 2 {
		t.Fatalf("expected the result and the image beside it, got %+v", request.Messages)
	}

	if request.Messages[0].Role != "tool" || request.Messages[0].Content != "picture.png" {
		t.Errorf("expected the tool's own text, got %+v", request.Messages[0])
	}

	if request.Messages[1].Role != "user" {
		t.Errorf("expected the image to arrive as a user message, got %+v", request.Messages[1])
	}

	if !strings.Contains(bodies[0], `"image_url":{"url":"data:image/png;base64,AQID"}`) {
		t.Errorf("expected the image to be carried as an image part, got %s", bodies[0])
	}
	if !strings.Contains(bodies[0], `"text":"Image attached."`) {
		t.Errorf("expected the attachment to explain the image, got %s", bodies[0])
	}
}

func TestATurnCutShortAgainstTheTokenLimitIsReported(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"content":"It is raining "}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
		"[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("what is the weather?")

	_, err := client.Send(t.Context(), func(agent.Output) bool { return true })
	if !errors.Is(err, chatcompletions.ErrIncomplete) {
		t.Fatalf("expected an incomplete response to be reported, got %v", err)
	}
}

func TestAMalformedChunkIsReportedRatherThanIgnored(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"content":"It is "}}]}`,
		`{"choices":[{"delta":`,
		"[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	_, err := client.Send(t.Context(), func(agent.Output) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("expected the malformed frame to be reported, got %v", err)
	}
}

func TestATopLevelErrorEnvelopeIsReported(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"model overloaded"}}`)

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	_, err := client.Send(t.Context(), func(agent.Output) bool { return true })
	if err == nil || err.Error() != "model overloaded" {
		t.Fatalf("expected the endpoint error, got %v", err)
	}
}

func TestAChunkCarryingNothingWeReadIsIgnored(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"id":"chunk-1","object":"chatcompletions.completion.chunk","system_fingerprint":"fp"}`,
		`{"choices":[{"delta":{"content":"done"}}]}`,
		"[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	var seen []string
	if _, err := client.Send(t.Context(), func(event agent.Output) bool {
		if !event.Done {
			seen = append(seen, event.Text)
		}

		return true
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(seen) != 1 || seen[0] != "done" {
		t.Errorf("expected a frame we read nothing from to pass quietly, got %v", seen)
	}
}

func TestHowCallsAreMadeIsStatedWhenThereAreToolsToCall(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies, `{"choices":[{"delta":{"content":"hi"}}]}`, "[DONE]")

	client := newClient(t, server.URL)
	client.Configure("", []tool.Definition{{Name: "weather", Description: "report the weather"}})
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"parallel_tool_calls":true`, `"tool_choice":"auto"`} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("expected %s to be sent rather than left to the endpoint, got %s", want, bodies[0])
		}
	}
}

func TestHowCallsAreMadeIsLeftUnsaidWhenThereAreNoTools(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies, `{"choices":[{"delta":{"content":"hi"}}]}`, "[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	for _, unwanted := range []string{"parallel_tool_calls", "tool_choice", "tools"} {
		if strings.Contains(bodies[0], unwanted) {
			t.Errorf("expected %s to be left out with nothing to call, got %s", unwanted, bodies[0])
		}
	}
}

func TestARefusalIsReadAsWhatTheModelSaid(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"refusal":"I cannot help with that."}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		"[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("do something forbidden")

	var seen []string
	if _, err := client.Send(t.Context(), func(event agent.Output) bool {
		if !event.Done {
			seen = append(seen, event.Text)
		}

		return true
	}); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 1 || seen[0] != "I cannot help with that." {
		t.Errorf("expected the refusal to be reported as what the model said, got %v", seen)
	}

	stored := client.Dump()
	if len(stored) != 2 || !strings.Contains(string(stored[1]), "I cannot help with that.") {
		t.Errorf("expected the refusal to be kept in the conversation, got %v", stored)
	}
}

func TestAnAnswerAFilterTookAwayIsReportedAsCutShort(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"content":"Here is how "}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`,
		"[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	_, err := client.Send(t.Context(), func(agent.Output) bool { return true })
	if !errors.Is(err, chatcompletions.ErrIncomplete) {
		t.Errorf("expected a filtered answer to be reported as cut short, got %v", err)
	}
}

func TestATurnThatProducedNothingIsNotStored(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies, `{"choices":[{"delta":{}}]}`, "[DONE]")

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	stored := client.Dump()
	if len(stored) != 1 {
		t.Fatalf("expected only the prompt to be stored, got %v", stored)
	}

	if strings.Contains(string(stored[0]), "assistant") {
		t.Errorf("expected no assistant message, got %s", stored[0])
	}
}

func TestAStreamingRequestAsksForAndReportsUsage(t *testing.T) {
	var bodies []string
	server := scriptedServer(
		t,
		&bodies,
		`{"choices":[{"delta":{"content":"hello"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":5000,"prompt_tokens_details":`+
			`{"cached_tokens":4000,"cache_write_tokens":300}}}`,
		"[DONE]",
	)

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")

	var outputs []agent.Output
	reply, err := client.Send(t.Context(), func(output agent.Output) bool {
		outputs = append(outputs, output)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(bodies[0], `"stream_options":{"include_usage":true}`) {
		t.Errorf("usage was not requested: %s", bodies[0])
	}
	if reply.Usage.InputTokens != 5000 || reply.Usage.Cache == nil ||
		reply.Usage.Cache.ReadTokens != 4000 || reply.Usage.Cache.WriteTokens != 300 {
		t.Errorf("got usage %#v", reply.Usage)
	}
	if len(outputs) == 0 || outputs[len(outputs)-1].Usage == nil ||
		outputs[len(outputs)-1].Usage.Cache == nil || outputs[len(outputs)-1].Usage.Cache.ReadTokens != 4000 {
		t.Errorf("final output did not carry usage: %#v", outputs)
	}
}

func TestConversationWithStreamingToolCall(t *testing.T) {
	type sentMessage struct {
		Role    string  `json:"role"`
		Content *string `json:"content"`
	}
	type sentRequest struct {
		Model           string        `json:"model"`
		ReasoningEffort string        `json:"reasoning_effort"`
		Messages        []sentMessage `json:"messages"`
	}

	var requests []sentRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("got authorisation %q", got)
		}

		var body sentRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body)

		writer.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"check\"}}]}\n\n")
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"rea\",\"arguments\":\"{\\\"pa\"}}]}}]}\n\n")
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"d\",\"arguments\":\"th\\\":\\\"x\\\"}\"}}]}}]}\n\n")
			_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
			return
		}

		_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := newClient(t, server.URL)
	client.Configure("be useful", []tool.Definition{{Name: "read", Description: "read a file"}})
	client.AddUserMessage("hello")

	var events []agent.Output
	reply, err := client.Send(t.Context(), func(event agent.Output) bool {
		events = append(events, event)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCall := agent.ToolCall{ID: "call-1", Name: "read", Arguments: `{"path":"x"}`}
	if len(reply.Calls) != 1 || reply.Calls[0] != wantCall {
		t.Fatalf("got calls %#v", reply.Calls)
	}
	if len(events) != 2 || events[0].Kind != agent.ModelReasoningEvent || events[0].Text != "check" ||
		events[1].Kind != agent.ModelReasoningEvent || !events[1].Done {
		t.Fatalf("got events %#v", events)
	}

	client.AddToolResults([]agent.ToolCallResult{{ID: "call-1", Output: "contents"}})
	events = nil
	if _, err := client.Send(t.Context(), func(event agent.Output) bool {
		events = append(events, event)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != agent.ModelMessageEvent || events[0].Text != "done" ||
		events[1].Kind != agent.ModelMessageEvent || !events[1].Done {
		t.Fatalf("got events %#v", events)
	}

	if got := requests[0].Model; got != "deepseek-v4-pro" {
		t.Errorf("got model %v", got)
	}
	if got := requests[0].ReasoningEffort; got != "high" {
		t.Errorf("got reasoning effort %v", got)
	}
	messages := requests[1].Messages
	roles := make([]string, len(messages))
	for i, message := range messages {
		roles[i] = message.Role
	}
	if want := []string{"system", "user", "assistant", "tool"}; !slices.Equal(roles, want) {
		t.Errorf("got roles %v", roles)
	}
	if messages[2].Content == nil || *messages[2].Content != "" {
		t.Errorf("got assistant content %v, want an explicit empty string", messages[2].Content)
	}
}

func TestATurnThatFailedKeepsWhatItSaidAndNotWhatItAskedFor(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"reasoning_content":"Checking the sky."}}]}`,
		`{"choices":[{"delta":{"content":"Looking it up."}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function",`+
			`"function":{"name":"weather","arguments":"{}"}}]}}]}`)

	client := newClient(t, server.URL)
	client.AddUserMessage("what is the weather?")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); !errors.Is(err, chatcompletions.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be refused, got %v", err)
	}

	var held strings.Builder
	for _, item := range client.Dump() {
		held.Write(item)
	}

	for _, want := range []string{"Checking the sky.", "Looking it up."} {
		if !strings.Contains(held.String(), want) {
			t.Errorf("expected the conversation to hold %q, got %s", want, held.String())
		}
	}

	if strings.Contains(held.String(), "call-1") {
		t.Errorf("expected an unanswerable call to be left out, got %s", held.String())
	}
}
