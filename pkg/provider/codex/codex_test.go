package codex_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/tool"
)

type WeatherParams struct {
	City string `json:"city"`
}

func events(payloads ...string) string {
	var out strings.Builder

	for _, payload := range payloads {
		fmt.Fprintf(&out, "data: %s\n\n", payload)
	}

	return out.String()
}

func answer(text string) string {
	return fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, text)
}

func call(name string, arguments string) string {
	item := fmt.Sprintf(
		`{"type":"function_call","call_id":%q,"name":%q,"arguments":%q}`, callID, name, arguments,
	)

	return fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, item)
}

const callID = "c1"

const completed = `{"type":"response.completed"}`

func completedWithUsage(inputTokens int, cacheReadTokens int, cacheWriteTokens int) string {
	return fmt.Sprintf(
		`{"type":"response.completed","response":{"usage":{"input_tokens":%d,`+
			`"input_tokens_details":{"cached_tokens":%d,"cache_write_tokens":%d}}}}`,
		inputTokens,
		cacheReadTokens,
		cacheWriteTokens,
	)
}

func turns(t *testing.T, scripted ...string) (*httptest.Server, *[]string) {
	t.Helper()

	var bodies []string
	var index int

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			bodies = append(bodies, string(body))

			if index >= len(scripted) {
				t.Errorf("the endpoint was asked %d times, with %d turns scripted",
					index+1, len(scripted))

				return
			}

			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(writer, scripted[index])
			index++
		},
	))

	t.Cleanup(server.Close)

	return server, &bodies
}

func newAgent(t *testing.T, url string, tools []tool.Tool) *agent.Agent {
	t.Helper()

	assistant := agent.New("You are a helpful assistant", newClient(t, url), tools)
	assistant.TakeRetryWaitsAtOnce()

	return assistant
}

func newClient(t *testing.T, url string) *codex.Client {
	t.Helper()

	client, err := codex.New(codex.Static("token", "account"), "gpt-5.6-sol", "high")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = url

	return client
}

func sendOnce(t *testing.T, client *codex.Client, message string) (string, error) {
	t.Helper()

	client.AddUserMessage(message)

	var said strings.Builder

	_, err := client.Send(t.Context(), func(output agent.Output) bool {
		if output.Kind == agent.ModelMessageEvent {
			said.WriteString(output.Text)
		}

		return true
	})

	return said.String(), err
}

func weatherTool(t *testing.T, callCount *int) tool.Tool {
	t.Helper()

	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args WeatherParams) (string, string) { return args.City, "" },
	).Plain(func(_ context.Context, args WeatherParams) (string, error) {
		*callCount++

		if args.City != "London" {
			t.Errorf("expected the city to be London, got %q", args.City)
		}

		return "raining in " + args.City, nil
	})
}

func TestNewHandsBackAClientHoldingWhatItWasAsked(t *testing.T) {
	client, err := codex.New(codex.Static("token", "account"), "gpt-5.6-sol", "low")
	if err != nil {
		t.Fatal(err)
	}

	if client.URL != codex.Endpoint || client.Model != "gpt-5.6-sol" || client.Effort != "low" {
		t.Errorf("expected what was asked for to be held verbatim, got %+v", client)
	}
}

func TestFastModeUsesTheUltrafastServiceTier(t *testing.T) {
	server, bodies := turns(t, events(answer("Fast."), completed), events(answer("Standard."), completed))
	client := newClient(t, server.URL)

	client.IsFast = true
	if _, err := sendOnce(t, client, "fast"); err != nil {
		t.Fatal(err)
	}
	client.IsFast = false
	if _, err := sendOnce(t, client, "standard"); err != nil {
		t.Fatal(err)
	}

	var fastRequest map[string]json.RawMessage
	if err := json.Unmarshal([]byte((*bodies)[0]), &fastRequest); err != nil {
		t.Fatal(err)
	}
	if got := string(fastRequest["service_tier"]); got != `"ultrafast"` {
		t.Errorf("got service tier %s", got)
	}

	var standardRequest map[string]json.RawMessage
	if err := json.Unmarshal([]byte((*bodies)[1]), &standardRequest); err != nil {
		t.Fatal(err)
	}
	if _, isFound := standardRequest["service_tier"]; isFound {
		t.Errorf("standard request carried a service tier: %s", (*bodies)[1])
	}
}

func TestASettingLeftOutIsRefusedRatherThanSubstituted(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		effort string
		want   string
	}{
		{"model", "", "high", "codex: Model is empty"},
		{"effort", "gpt-5.6-sol", "", "codex: Effort is empty"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := codex.New(codex.Static("token", "account"), test.model, test.effort)

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}

			if client != nil {
				t.Errorf("expected no client to be handed back, got %+v", client)
			}
		})
	}
}

func TestAuthRefusesWhateverNewWould(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	client, err := codex.Auth("", "high")
	if err == nil || !strings.Contains(err.Error(), "codex: Model is empty") {
		t.Fatalf("expected the missing model to be refused, got %v", err)
	}

	if client != nil {
		t.Errorf("expected no client to be handed back, got %+v", client)
	}
}

func TestDumpAndLoadOwnTheirHistorySlices(t *testing.T) {
	client, err := codex.New(codex.Static("token", "account"), "gpt-5.6-sol", "high")
	if err != nil {
		t.Fatal(err)
	}
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
			client, err := codex.New(codex.Static("token", "account"), "gpt-5.6-sol", "high")
			if err != nil {
				t.Fatal(err)
			}
			client.AddToolResults([]agent.ToolCallResult{{ID: "call-1", Output: "text", Image: image}})

			if got := string(client.Dump()[0]); strings.Contains(got, "input_image") {
				t.Errorf("partial image was sent: %s", got)
			}
		})
	}
}

func TestAnOversizedImageIsScaledBeforeItIsSent(t *testing.T) {
	server, bodies := turns(t, events(answer("Seen."), completed))

	client := newClient(t, server.URL)
	client.AddToolResults([]agent.ToolCallResult{{ID: "call-1", Output: "text", Image: oversizedPNG(t)}})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	requireBoundedImage(t, (*bodies)[0], "base64,")
}

func TestAnOversizedImageInStoredHistoryIsScaledBeforeItIsSent(t *testing.T) {
	server, bodies := turns(t, events(answer("Seen."), completed))

	client := newClient(t, server.URL)
	client.Load([]json.RawMessage{storedImageItem(t)})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	requireBoundedImage(t, (*bodies)[0], "base64,")
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
		`{"type":"function_call_output","call_id":"call-1","output":[{"type":"input_image","image_url":"data:image/png;base64,` +
			base64.StdEncoding.EncodeToString(oversizedPNG(t).Data) + `","detail":"high"}]}`,
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

func TestSendRunsToolsUntilTheModelStops(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("weather", `{"city":"London"}`), completed),
		events(answer("It is raining "), answer("in London."), completed),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer != "It is raining in London." {
		t.Errorf("expected the answer, got %q", answer)
	}

	if callCount != 1 {
		t.Errorf("expected the tool to run once, ran %d times", callCount)
	}

	if len(*bodies) != 2 {
		t.Fatalf("expected two requests, got %d", len(*bodies))
	}

	var promptCacheKey string
	for i, body := range *bodies {
		var request struct {
			PromptCacheKey string `json:"prompt_cache_key"`
		}
		if err := json.Unmarshal([]byte(body), &request); err != nil {
			t.Fatal(err)
		}
		if request.PromptCacheKey == "" {
			t.Errorf("request %d has no prompt cache key", i+1)
		}
		if promptCacheKey != "" && request.PromptCacheKey != promptCacheKey {
			t.Errorf("prompt cache key changed from %q to %q", promptCacheKey, request.PromptCacheKey)
		}
		promptCacheKey = request.PromptCacheKey
	}

	if !strings.Contains((*bodies)[1], "function_call_output") {
		t.Error("expected the second request to carry the tool result")
	}

	if !strings.Contains((*bodies)[0], "You are a helpful assistant") {
		t.Error("expected the request to carry the instructions")
	}

	if !strings.Contains((*bodies)[0], `"name":"weather"`) {
		t.Error("expected the request to declare the tool")
	}
}

func TestAnImageReturnedByAToolIsSentForTheModelToInspect(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("view", `{}`), completed),
		events(answer("I can see it."), completed),
	)

	type nothing struct{}
	viewTool := tool.Implement(
		tool.Definition{
			Name:        "view",
			Description: "view an image",
			Schema:      tool.Schema{},
		},
		func(nothing) (string, string) { return "picture.png", "" },
	).Run(func(context.Context, nothing) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{
			Output: "image/png image (3 bytes)",
			Image: tool.Image{
				MediaType: "image/png",
				Data:      []byte{1, 2, 3},
			},
		}, nil
	})

	assistant := newAgent(t, server.URL, []tool.Tool{viewTool})
	if _, err := assistant.Send(t.Context(), "inspect the image"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `"output":[{"type":"input_text","text":"image/png image (3 bytes)"},` +
		`{"type":"input_text","text":"Image attached."},` +
		`{"type":"input_image","image_url":"data:image/png;base64,AQID","detail":"high"}]`
	if !strings.Contains((*bodies)[1], want) {
		t.Errorf("expected the tool output to carry an image, got %s", (*bodies)[1])
	}
}

func TestNoOutputCeilingIsSent(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	if _, err := newAgent(t, server.URL, nil).Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, unwanted := range []string{"max_output_tokens", "max_tokens", "max_completion_tokens"} {
		if strings.Contains((*bodies)[0], unwanted) {
			t.Errorf("expected no %s to be sent, got %s", unwanted, (*bodies)[0])
		}
	}
}

func TestARefusalSaidFlatlyIsReportedInTheEndpointsOwnWords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(writer, `{"detail":"Unsupported parameter: max_output_tokens"}`)
		},
	))

	t.Cleanup(server.Close)

	_, err := newAgent(t, server.URL, nil).Send(t.Context(), "hello")
	if err == nil || err.Error() != "Unsupported parameter: max_output_tokens" {
		t.Errorf("expected the endpoint's own words, got %v", err)
	}
}

func TestToolsAreOfferedInTheResponsesShape(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolsJSON := `"tools":[{"type":"function","name":"weather",` +
		`"description":"report weather in a city","strict":false,` +
		`"parameters":{"type":"object","properties":{"city":` +
		`{"type":"string","description":"the city to look up"}},` +
		`"required":["city"],"additionalProperties":false}}]`

	if !strings.Contains((*bodies)[0], toolsJSON) {
		t.Errorf("expected %s, got %s", toolsJSON, (*bodies)[0])
	}
}

func TestAToolWithNoArgumentsIsStillGivenASchema(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	type nothing struct{}

	waitingTool := tool.Implement(
		tool.Definition{
			Name:        "wait",
			Description: "wait for something to happen",
			Schema:      tool.Schema{},
		},
		func(nothing) (string, string) { return "", "" },
	).Plain(func(context.Context, nothing) (string, error) { return "", nil })

	assistant := newAgent(t, server.URL, []tool.Tool{waitingTool})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schemaJSON := `"parameters":{"type":"object","properties":{},"additionalProperties":false}`

	if !strings.Contains((*bodies)[0], schemaJSON) {
		t.Errorf("expected %s, got %s", schemaJSON, (*bodies)[0])
	}
}

func TestStreamReportsEachTurnAsItHappens(t *testing.T) {
	server, _ := turns(
		t,
		events(answer("Let me look. "), call("weather", `{"city":"London"}`), completed),
		events(answer("It is raining in London."), completed),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	var eventStrings []string

	for update, err := range assistant.Stream(t.Context(), "what is the weather in London?", nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if update.Event == nil {
			continue
		}

		event := update.Event
		eventStrings = append(eventStrings, fmt.Sprintf("%s:%s:%s", event.Kind, event.Name, event.Text))
	}

	expectedEvents := []string{
		fmt.Sprintf("%s::what is the weather in London?", agent.UserMessageEvent),
		fmt.Sprintf("%s::Let me look. ", agent.ModelMessageEvent),
		fmt.Sprintf("%s:weather:", agent.ToolCallRequestEvent),
		fmt.Sprintf("%s:weather:raining in London", agent.ToolCallResultEvent),
		fmt.Sprintf("%s::It is raining in London.", agent.ModelMessageEvent),
	}

	if !slices.Equal(eventStrings, expectedEvents) {
		t.Errorf("expected %v, got %v", expectedEvents, eventStrings)
	}
}

func TestStreamAttachesEachRequestsContextUsageToItsFinalEvent(t *testing.T) {
	server, _ := turns(
		t,
		events(call("weather", `{"city":"London"}`), completedWithUsage(12_000, 8_000, 500)),
		events(answer("It is raining."), completedWithUsage(27_400, 24_000, 700)),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	var usages []agent.Usage
	for update, err := range assistant.Stream(t.Context(), "what is the weather?", nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if update.Event != nil && update.Event.Usage != nil {
			usages = append(usages, *update.Event.Usage)
		}
	}

	want := []agent.Usage{
		{InputTokens: 12_000, Cache: &agent.CacheUsage{ReadTokens: 8_000, WriteTokens: 500}},
		{InputTokens: 27_400, Cache: &agent.CacheUsage{ReadTokens: 24_000, WriteTokens: 700}},
	}
	if !reflect.DeepEqual(usages, want) {
		t.Errorf("got usage %v, want %v", usages, want)
	}
}

func TestStreamStopsWhenTheCallerDoes(t *testing.T) {
	server, bodies := turns(
		t,
		events(answer("It is raining "), call("weather", `{"city":"London"}`), completed),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	var eventCount int

	for update := range assistant.Stream(t.Context(), "what is the weather in London?", nil) {
		eventCount++

		if update.Delta != nil && update.Delta.Kind == agent.ModelMessageEvent {
			break
		}
	}

	if eventCount != 2 {
		t.Errorf("expected the prompt and one answer, got %d events", eventCount)
	}

	if callCount != 0 {
		t.Errorf("expected the tool not to run, ran %d times", callCount)
	}

	if len(*bodies) != 1 {
		t.Errorf("expected one request, got %d", len(*bodies))
	}
}

func TestStreamReportsReasoningApartFromTheAnswer(t *testing.T) {
	server, bodies := turns(
		t,
		events(
			`{"type":"response.reasoning_summary_text.delta","delta":"**Adjusting Network Width and Formatting Logic**"}`,
			`{"type":"response.reasoning_summary_part.done",`+
				`"part":{"type":"summary_text","text":"**Adjusting Network Width and Formatting Logic**"}}`,
			`{"type":"response.reasoning_summary_text.delta","delta":"**Copying and Preparing Network Source Code**"}`,
			`{"type":"response.reasoning_summary_part.done",`+
				`"part":{"type":"summary_text","text":"**Copying and Preparing Network Source Code**"}}`,
			answer("It is raining."),
			completed,
		),
	)

	assistant := newAgent(t, server.URL, nil)

	var reasoningDeltas strings.Builder
	var reasoningSummaries []string
	var answer strings.Builder

	for update, err := range assistant.Stream(t.Context(), "what is the weather?", nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if update.Delta != nil && update.Delta.Kind == agent.ModelReasoningEvent {
			reasoningDeltas.WriteString(update.Delta.Text)
		}
		if update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent {
			reasoningSummaries = append(reasoningSummaries, update.Event.Text)
		}
		if update.Event != nil && update.Event.Kind == agent.ModelMessageEvent {
			answer.WriteString(update.Event.Text)
		}
	}

	if reasoningDeltas.String() != "**Adjusting Network Width and Formatting Logic****Copying and Preparing Network Source Code**" {
		t.Errorf("expected both summaries to stream, got %q", reasoningDeltas.String())
	}
	wantSummaries := []string{
		"**Adjusting Network Width and Formatting Logic**",
		"**Copying and Preparing Network Source Code**",
	}
	if !slices.Equal(reasoningSummaries, wantSummaries) {
		t.Errorf("expected %v, got %v", wantSummaries, reasoningSummaries)
	}

	if want := "It is raining."; answer.String() != want {
		t.Errorf("expected the answer alone, got %q", answer.String())
	}

	if !strings.Contains((*bodies)[0], `"reasoning":{"effort":"high","summary":"auto"}`) {
		t.Errorf("expected a summary to have been asked for, got %s", (*bodies)[0])
	}
}

func TestSendReportsAnEndpointFailure(t *testing.T) {
	server, _ := turns(
		t,
		events(`{"type":"response.failed","response":{"error":{"message":"model overloaded"}}}`),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "hello"); err == nil || err.Error() != "model overloaded" {
		t.Errorf("expected the endpoint's own message, got %v", err)
	}
}

const refusalPayload = `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_prompt","message":"Your prompt was flagged.","param":null},"sequence_number":2}`

func TestSendReportsADirectEndpointFailure(t *testing.T) {
	server, _ := turns(t, events(refusalPayload))
	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "hello"); err == nil ||
		err.Error() != "Your prompt was flagged." {
		t.Errorf("expected the endpoint's own message, got %v", err)
	}
}

func TestADirectEndpointFailureCarriesItsCode(t *testing.T) {
	server, _ := turns(t, events(refusalPayload))
	assistant := newAgent(t, server.URL, nil)

	_, err := assistant.Send(t.Context(), "hello")

	var failure *codex.StreamError
	if !errors.As(err, &failure) {
		t.Fatalf("expected a typed stream error, got %v", err)
	}

	if failure.Code != "invalid_prompt" {
		t.Errorf("expected the endpoint's own code, got %q", failure.Code)
	}
}

func TestATransientEndpointFailureIsAskedAgain(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "server error",
			payload: `{"type":"error","error":{"type":"server_error","code":"server_error","message":"An error occurred while processing your request.","param":null},"sequence_number":2}`,
		},
		{
			name:    "service unavailable",
			payload: `{"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later.","param":null},"sequence_number":2}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, bodies := turns(
				t,
				events(test.payload),
				events(answer("Through on the second attempt."), completed),
			)

			assistant := newAgent(t, server.URL, nil)

			reply, err := assistant.Send(t.Context(), "hello")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if reply != "Through on the second attempt." {
				t.Errorf("expected the second attempt to be handed back, got %q", reply)
			}

			if len(*bodies) != 2 {
				t.Errorf("expected the failure to be asked again once, got %d requests", len(*bodies))
			}
		})
	}
}

func TestSendShowsAnEndpointFailureWithoutAMessageAsJSON(t *testing.T) {
	server, _ := turns(t, events(`{"type":"error","error":{"code":"overloaded","retry":true}}`))
	assistant := newAgent(t, server.URL, nil)

	_, err := assistant.Send(t.Context(), "hello")
	want := `{
  "type": "error",
  "error": {
    "code": "overloaded",
    "retry": true
  }
}`
	if err == nil || err.Error() != want {
		t.Errorf("expected the whole event as JSON, got %v", err)
	}
}

func TestSendShowsAnEndpointFailureWithAnUnreadableMessageAsJSON(t *testing.T) {
	server, _ := turns(t, events(`{"type":"error","message":{"text":"model overloaded"}}`))
	assistant := newAgent(t, server.URL, nil)

	_, err := assistant.Send(t.Context(), "hello")
	want := `{
  "type": "error",
  "message": {
    "text": "model overloaded"
  }
}`
	if err == nil || err.Error() != want {
		t.Errorf("expected the whole event as JSON, got %v", err)
	}
}

func TestStreamReportsRawReasoningFromAModelThatDoesNotSummarise(t *testing.T) {
	server, _ := turns(
		t,
		events(
			`{"type":"response.reasoning_text.delta","delta":"Chec"}`,
			`{"type":"response.reasoning_text.delta","delta":"king."}`,
			answer("It is raining."),
			completed,
		),
	)

	assistant := newAgent(t, server.URL, nil)

	var reasoning strings.Builder
	for update, err := range assistant.Stream(t.Context(), "what is the weather?", nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if update.Event == nil {
			continue
		}
		if update.Event.Kind == agent.ModelReasoningEvent {
			reasoning.WriteString(update.Event.Text)
		}
	}

	if reasoning.String() != "Checking." {
		t.Errorf("expected the raw reasoning, got %q", reasoning.String())
	}
}

func TestASummarisedThoughtIsNotAlsoReportedRaw(t *testing.T) {
	server, _ := turns(
		t,
		events(
			`{"type":"response.reasoning_summary_part.done",`+
				`"part":{"type":"summary_text","text":"Checking the sky."}}`,
			`{"type":"response.reasoning_text.delta","delta":"Checking the sky in full."}`,
			answer("It is raining."),
			completed,
		),
	)

	assistant := newAgent(t, server.URL, nil)

	var reasoning []string
	for update, err := range assistant.Stream(t.Context(), "what is the weather?", nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if update.Event == nil {
			continue
		}
		if update.Event.Kind == agent.ModelReasoningEvent {
			reasoning = append(reasoning, update.Event.Text)
		}
	}

	if len(reasoning) != 1 || reasoning[0] != "Checking the sky." {
		t.Errorf("expected the summary alone, got %v", reasoning)
	}
}

func TestARefusalIsShownRatherThanSwallowed(t *testing.T) {
	server, _ := turns(
		t,
		events(
			`{"type":"response.refusal.delta","delta":"I cannot help with that."}`,
			completed,
		),
	)

	assistant := newAgent(t, server.URL, nil)

	reply, err := assistant.Send(t.Context(), "do something forbidden")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "I cannot help with that." {
		t.Errorf("expected the refusal to reach the caller, got %q", reply)
	}
}

func TestSendRefusesATruncatedStream(t *testing.T) {
	server, _ := turns(t, events(answer("It is raining ")))

	client := newClient(t, server.URL)

	answer, err := sendOnce(t, client, "what is the weather in London?")
	if err == nil {
		t.Fatal("expected a truncated stream to be refused")
	}

	if answer != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", answer)
	}
}

func TestSendReportsAnIncompleteResponse(t *testing.T) {
	server, _ := turns(
		t,
		events(answer("It is raining "), `{"type":"response.incomplete"}`),
	)

	assistant := newAgent(t, server.URL, nil)

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if !errors.Is(err, codex.ErrIncomplete) {
		t.Fatalf("expected an incomplete response to be reported, got %v", err)
	}

	if answer != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", answer)
	}
}

func TestSendAcceptsTheDoneSentinel(t *testing.T) {
	server, _ := turns(t, events(answer("It is raining in London."), "[DONE]"))

	assistant := newAgent(t, server.URL, nil)

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer != "It is raining in London." {
		t.Errorf("expected the answer, got %q", answer)
	}
}

func TestSendTellsTheModelWhenThereIsNoSuchTool(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("missing", `{}`), completed),
		events(answer("Sorry."), completed),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[1], `there is no tool called \"missing\"`) {
		t.Errorf("expected the model to be told, got %s", (*bodies)[1])
	}
}

func TestCancellingTheContextEndsARequestThatIsProducingNothing(t *testing.T) {
	requestDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")

			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}

			<-request.Context().Done()
			close(requestDone)
		},
	))

	t.Cleanup(server.Close)

	ctx, stop := context.WithCancel(t.Context())

	failure := make(chan error, 1)

	go func() {
		_, err := newAgent(t, server.URL, nil).Send(ctx, "are you there")
		failure <- err
	}()

	time.Sleep(50 * time.Millisecond)
	stop()

	select {
	case err := <-failure:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected the turn to report the cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected the request to end when the context was cancelled")
	}

	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Error("expected the endpoint to see the connection go")
	}
}

func message(text string) string {
	item := fmt.Sprintf(
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}`, text,
	)

	return fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, item)
}

func thought(id string) string {
	item := fmt.Sprintf(`{"type":"reasoning","id":%q,"summary":[]}`, id)

	return fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, item)
}

func TestATurnThatFailedForGoodKeepsTheAnswerItHadBegun(t *testing.T) {
	server, _ := turns(t, events(
		answer("Half an ans"),
		`{"type":"response.failed","response":{"error":{"code":"invalid_prompt","message":"refused"}}}`,
	))

	client := newClient(t, server.URL)

	if _, err := sendOnce(t, client, "what is the weather?"); err == nil {
		t.Fatal("expected the turn to fail")
	}

	var held strings.Builder
	for _, item := range client.Dump() {
		held.Write(item)
	}

	if !strings.Contains(held.String(), "Half an ans") {
		t.Errorf("expected the conversation to hold what was said, got %s", held.String())
	}
}

func TestATurnThatWillBeAskedAgainForgetsTheAnswerItHadBegun(t *testing.T) {
	server, _ := turns(t, events(
		answer("Half an ans"),
		`{"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"overloaded"}}}`,
	))

	client := newClient(t, server.URL)

	if _, err := sendOnce(t, client, "what is the weather?"); err == nil {
		t.Fatal("expected the turn to fail")
	}

	var held strings.Builder
	for _, item := range client.Dump() {
		held.Write(item)
	}

	if strings.Contains(held.String(), "Half an ans") {
		t.Errorf("expected the abandoned attempt to be left out, got %s", held.String())
	}
}

func TestAConfirmedAnswerIsHeldOnceWhenTheTurnLaterFails(t *testing.T) {
	server, _ := turns(t, events(
		answer("Looking it up."),
		message("Looking it up."),
		`{"type":"response.failed","response":{"error":{"code":"invalid_prompt","message":"refused"}}}`,
	))

	client := newClient(t, server.URL)

	if _, err := sendOnce(t, client, "what is the weather?"); err == nil {
		t.Fatal("expected the turn to fail")
	}

	if held := len(client.Dump()); held != 2 {
		t.Errorf("expected the user message and one answer, got %d items", held)
	}
}

func TestATurnThatFailedKeepsWhatItSaidAndNotWhatItAskedFor(t *testing.T) {
	server, _ := turns(t, events(
		thought("rs_kept"),
		message("Looking it up."),
		thought("rs_orphaned"),
		call("weather", `{"city":"London"}`),
	))

	client := newClient(t, server.URL)

	if _, err := sendOnce(t, client, "what is the weather?"); !errors.Is(err, codex.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be refused, got %v", err)
	}

	var held strings.Builder
	for _, item := range client.Dump() {
		held.Write(item)
	}

	for _, want := range []string{"Looking it up.", "rs_kept"} {
		if !strings.Contains(held.String(), want) {
			t.Errorf("expected the conversation to hold %q, got %s", want, held.String())
		}
	}

	for _, unwanted := range []string{"function_call", "rs_orphaned"} {
		if strings.Contains(held.String(), unwanted) {
			t.Errorf("expected %q to be left out, got %s", unwanted, held.String())
		}
	}
}
