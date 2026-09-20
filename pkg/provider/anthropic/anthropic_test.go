package anthropic_test

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
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/anthropic"
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

const messageStart = `{"type":"message_start","message":{"id":"msg_1","role":"assistant","content":[]}}`

const messageStop = `{"type":"message_stop"}`

func textStart(index int) string {
	return fmt.Sprintf(`{"type":"content_block_start","index":%d,`+
		`"content_block":{"type":"text","text":""}}`, index)
}

func textDelta(index int, text string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,`+
		`"delta":{"type":"text_delta","text":%q}}`, index, text)
}

//nolint:unparam // the index is part of the shape, and every block helper here takes one
func thinkingStart(index int) string {
	return fmt.Sprintf(`{"type":"content_block_start","index":%d,`+
		`"content_block":{"type":"thinking","thinking":""}}`, index)
}

//nolint:unparam // the index is part of the shape, and every block helper here takes one
func thinkingDelta(index int, text string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,`+
		`"delta":{"type":"thinking_delta","thinking":%q}}`, index, text)
}

//nolint:unparam // the index is part of the shape, and every block helper here takes one
func signatureDelta(index int, signature string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,`+
		`"delta":{"type":"signature_delta","signature":%q}}`, index, signature)
}

func toolStart(index int, id string, name string) string {
	return fmt.Sprintf(`{"type":"content_block_start","index":%d,`+
		`"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, index, id, name)
}

func argumentsDelta(index int, fragment string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,`+
		`"delta":{"type":"input_json_delta","partial_json":%q}}`, index, fragment)
}

func blockStop(index int) string {
	return fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index)
}

func stop(reason string) string {
	return fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{}}`, reason)
}

func startWithUsage(fresh int, cacheRead int, cacheCreation int) string {
	return fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","role":"assistant",`+
		`"content":[],"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":%d,"output_tokens":1}}}`, fresh, cacheRead, cacheCreation)
}

func stopWithUsage(reason string, fresh int, cacheRead int, cacheCreation int) string {
	return fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},`+
		`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":%d,"output_tokens":510}}`,
		reason, fresh, cacheRead, cacheCreation)
}

func answer(text string) []string {
	return []string{
		messageStart, textStart(0), textDelta(0, text), blockStop(0),
		stop("end_turn"), messageStop,
	}
}

func toolTurn(index int, id string, name string, arguments string) []string {
	return []string{
		toolStart(index, id, name),
		argumentsDelta(index, arguments),
		blockStop(index),
	}
}

func script(parts ...[]string) string {
	var payloads []string
	for _, part := range parts {
		payloads = append(payloads, part...)
	}

	return events(payloads...)
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

func newClient(t *testing.T, url string) *anthropic.Client {
	t.Helper()

	client, err := anthropic.New(anthropic.Static("sk-ant-oat-fake"), "claude-opus-5", "high", 128_000)
	if err != nil {
		t.Fatal(err)
	}
	client.URL = url

	return client
}

func TestNewHandsBackAClientHoldingWhatItWasAsked(t *testing.T) {
	client, err := anthropic.New(anthropic.Static("sk-ant-oat-fake"), "claude-haiku-4-5", "low", 64_000)
	if err != nil {
		t.Fatal(err)
	}

	if client.URL != anthropic.Endpoint || client.Model != "claude-haiku-4-5" ||
		client.Effort != "low" || client.MaxOutputTokens != 64_000 {
		t.Errorf("expected what was asked for to be held verbatim, got %+v", client)
	}
}

func TestAuthRefusesWhateverNewWould(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	client, err := anthropic.Auth("", "high", 128_000)
	if err == nil || !strings.Contains(err.Error(), "anthropic: Model is empty") {
		t.Fatalf("expected the missing model to be refused, got %v", err)
	}

	if client != nil {
		t.Errorf("expected no client to be handed back, got %+v", client)
	}
}

func TestASettingLeftOutIsRefusedRatherThanSubstituted(t *testing.T) {
	tests := []struct {
		name            string
		model           string
		effort          string
		maxOutputTokens int
		want            string
	}{
		{"model", "", "high", 128_000, "anthropic: Model is empty"},
		{"effort", "claude-opus-5", "", 128_000, "anthropic: Effort is empty"},
		{"max tokens", "claude-opus-5", "high", 0, "anthropic: MaxOutputTokens is 0"},
		{"unknown effort", "claude-opus-5", "enormous", 128_000, `anthropic: Effort is "enormous"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := anthropic.New(
				anthropic.Static("sk-ant-oat-fake"), test.model, test.effort, test.maxOutputTokens,
			)

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
	client, err := anthropic.New(anthropic.Static("token"), "claude-opus-5", "high", 128_000)
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
			client, err := anthropic.New(anthropic.Static("token"), "claude-opus-5", "high", 128_000)
			if err != nil {
				t.Fatal(err)
			}
			client.AddToolResults([]agent.ToolCallResult{{ID: "toolu_1", Output: "text", Image: image}})

			if got := string(client.Dump()[0]); strings.Contains(got, `"type":"image"`) {
				t.Errorf("partial image was sent: %s", got)
			}
		})
	}
}

func TestAnOversizedImageIsScaledBeforeItIsSent(t *testing.T) {
	server, bodies := turns(t, script(answer("Seen.")))

	client := newClient(t, server.URL)
	client.AddToolResults([]agent.ToolCallResult{{ID: "toolu_1", Output: "text", Image: oversizedPNG(t)}})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	requireBoundedImage(t, (*bodies)[0], `"data":"`)
}

func TestAnOversizedImageInStoredHistoryIsScaledBeforeItIsSent(t *testing.T) {
	server, bodies := turns(t, script(answer("Seen.")))

	client := newClient(t, server.URL)
	client.Load([]json.RawMessage{storedImageItem(t)})

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	requireBoundedImage(t, (*bodies)[0], `"data":"`)
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
		`{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` +
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

func TestTheModelListingFollowsTheEndpointItWasGiven(t *testing.T) {
	var asked string

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			asked = request.URL.Path

			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"data":[{"type":"model","id":"claude-opus-5",`+
				`"display_name":"Claude Opus 5","max_input_tokens":1000,"max_tokens":500,`+
				`"capabilities":{"effort":{"supported":true,`+
				`"low":{"supported":true},"high":{"supported":true}}}}]}`)
		},
	))

	t.Cleanup(server.Close)

	client := newClient(t, server.URL+"/v1/messages")

	models, err := client.Models(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if asked != "/v1/models" {
		t.Errorf("expected the listing beside the turn address, got %q", asked)
	}

	if len(models) != 1 {
		t.Fatalf("expected one model, got %v", models)
	}

	want := agent.Model{
		ID:                  "claude-opus-5",
		Name:                "Claude Opus 5",
		EffortLevels:        []string{"low", "high"},
		ContextWindowTokens: 1000,
		MaxOutputTokens:     500,
	}
	if models[0].ID != want.ID || models[0].Name != want.Name ||
		models[0].ContextWindowTokens != want.ContextWindowTokens ||
		models[0].MaxOutputTokens != want.MaxOutputTokens ||
		!slices.Equal(models[0].EffortLevels, want.EffortLevels) {
		t.Errorf("expected %+v, got %+v", want, models[0])
	}
}

func TestWhichModelsAreTakenToThinkAdaptively(t *testing.T) {
	for id, want := range map[string]bool{
		"claude-opus-5":              true,
		"claude-fable-5":             true,
		"claude-opus-4-8":            true,
		"claude-opus-4-6":            true,
		"claude-sonnet-4-6":          true,
		"claude-mythos-preview":      true,
		"claude-opus-4-5":            false,
		"claude-opus-4-5-20251101":   false,
		"claude-sonnet-4-5-20250929": false,
		"claude-haiku-4-5":           false,
		"claude-3-7-sonnet-20250219": false,
		"claude-3-5-haiku-20241022":  false,
	} {
		if got := anthropic.SupportsAdaptiveThinking(id); got != want {
			t.Errorf("expected %s to be %v, got %v", id, want, got)
		}
	}
}

func TestAModelListingIsNotAttemptedAgainstAnUnrecognisedEndpoint(t *testing.T) {
	client := newClient(t, "http://127.0.0.1:1/somewhere/else")

	models, err := client.Models(t.Context())
	if err != nil || models != nil {
		t.Errorf("expected no listing to be attempted, got %v and %v", models, err)
	}
}

func newAgent(t *testing.T, url string, tools []tool.Tool) *agent.Agent {
	t.Helper()

	assistant := agent.New("You are a helpful assistant", newClient(t, url), tools)
	assistant.TakeRetryWaitsAtOnce()

	return assistant
}

type wireMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

func requestMessages(t *testing.T, body string) []wireMessage {
	t.Helper()

	var request struct {
		Messages []wireMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatal(err)
	}

	return request.Messages
}

func lastMessage(t *testing.T, body string) wireMessage {
	t.Helper()

	messages := requestMessages(t, body)
	if len(messages) == 0 {
		t.Fatalf("expected the request to carry a conversation, got %s", body)
	}

	return messages[len(messages)-1]
}

type wireToolResult struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func toolResults(t *testing.T, body string) []wireToolResult {
	t.Helper()

	results := make([]wireToolResult, 0)

	for _, block := range lastMessage(t, body).Content {
		var result wireToolResult
		if err := json.Unmarshal(block, &result); err != nil {
			t.Fatal(err)
		}
		if result.Type == "tool_result" {
			results = append(results, result)
		}
	}

	return results
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

		return "raining in " + args.City, nil
	})
}

type nothing struct{}

func emptyTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: name, Description: "do the thing", Schema: tool.Schema{}},
		func(nothing) (string, string) { return "", "" },
	).Plain(func(context.Context, nothing) (string, error) { return "done", nil })
}

func TestARequestCarriesWhatTheMessagesAPIExpects(t *testing.T) {
	server, bodies := turns(t, script(answer("It is raining.")))

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	reply, err := assistant.Send(t.Context(), "what is the weather?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "It is raining." {
		t.Errorf("expected the answer, got %q", reply)
	}

	body := (*bodies)[0]

	for _, want := range []string{
		`"max_tokens":128000`,
		`"stream":true`,
		`"thinking":{"type":"adaptive","display":"summarized"}`,
		`"output_config":{"effort":"high"}`,
		`"model":"claude-opus-5"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected the request to carry %s, got %s", want, body)
		}
	}
}

func TestTheSystemPromptOpensWithTheClaudeCodeIdentity(t *testing.T) {
	server, bodies := turns(t, script(answer("Hello.")))

	assistant := newAgent(t, server.URL, nil)
	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var request struct {
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
	}
	if err := json.Unmarshal([]byte((*bodies)[0]), &request); err != nil {
		t.Fatal(err)
	}

	if len(request.System) != 2 {
		t.Fatalf("expected the identity and the instructions, got %d blocks", len(request.System))
	}

	if request.System[0].Text != anthropic.Identity {
		t.Errorf("expected the identity first, got %q", request.System[0].Text)
	}

	if request.System[1].Text != "You are a helpful assistant" {
		t.Errorf("expected the instructions second, got %q", request.System[1].Text)
	}
}

func TestCacheBreakpointsAreAppliedOnTheWayOutAndNeverStored(t *testing.T) {
	server, bodies := turns(t, script(answer("Hello.")))

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := strings.Count((*bodies)[0], `"cache_control"`); got != 4 {
		t.Errorf("expected the four breakpoints the endpoint allows, got %d in %s", got, (*bodies)[0])
	}

	if !strings.Contains((*bodies)[0], `"cache_control":{"type":"ephemeral"},"system"`) {
		t.Errorf("expected the conversation breakpoint to be left to the endpoint, got %s", (*bodies)[0])
	}

	for _, block := range requestBlocks(t, (*bodies)[0]) {
		if _, isMarked := block["cache_control"]; isMarked {
			t.Errorf("expected no breakpoint written into the conversation, got %v", block)
		}
	}

	state, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range state {
		if strings.Contains(string(item), "cache_control") {
			t.Errorf("expected stored state to carry no breakpoint, got %s", item)
		}
	}
}

func TestACallIsAssembledFromItsArgumentFragments(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart, toolStart(0, "toolu_1", "weather")},
			[]string{
				argumentsDelta(0, `{"ci`),
				argumentsDelta(0, `ty":"Lon`),
				argumentsDelta(0, `don"}`),
				blockStop(0),
				stop("tool_use"),
				messageStop,
			},
		),
		script(answer("It is raining in London.")),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "what is the weather in London?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected the tool to run once, ran %d times", callCount)
	}

	results := toolResults(t, (*bodies)[1])
	if len(results) != 1 {
		t.Fatalf("expected one result, got %v", results)
	}

	if results[0].ToolUseID != "toolu_1" || string(results[0].Content) != `"raining in London"` {
		t.Errorf("expected the call to be answered with what it returned, got %+v", results[0])
	}
}

func TestTwoCallsInOneTurnAreAnsweredInOneMessage(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(1, "toolu_1", "weather", `{"city":"London"}`),
			toolTurn(2, "toolu_2", "weather", `{"city":"Paris"}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("Both are wet.")),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "what is the weather?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected both calls to run, ran %d", callCount)
	}

	answered := lastMessage(t, (*bodies)[1])
	if answered.Role != "user" || len(answered.Content) != 2 {
		t.Fatalf("expected one user message holding both results, got %+v", answered)
	}

	results := toolResults(t, (*bodies)[1])
	if len(results) != 2 || results[0].ToolUseID != "toolu_1" || results[1].ToolUseID != "toolu_2" {
		t.Errorf("expected both calls to be answered, got %+v", results)
	}
}

func TestACallTakingNothingIsReadAsAnEmptyObject(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart, toolStart(0, "toolu_1", "wait"), blockStop(0)},
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("Waited.")),
	)

	assistant := newAgent(t, server.URL, []tool.Tool{emptyTool("wait")})

	if _, err := assistant.Send(t.Context(), "wait for it"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[1], `"input":{}`) {
		t.Errorf("expected the stored call to carry an empty object, got %s", (*bodies)[1])
	}
}

func TestAMalformedCallIsRetriedWithoutEnteringHistory(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(0, "toolu_1", "weather", `{"city":"London",,}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("I corrected the call.")),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "what is the weather?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 0 {
		t.Errorf("expected the malformed call not to run, ran %d times", callCount)
	}
	if len(*bodies) != 2 {
		t.Fatalf("expected the malformed response to be retried once, got %d requests", len(*bodies))
	}
	if strings.Contains((*bodies)[1], "toolu_1") || strings.Contains((*bodies)[1], `{"city":"London",,}`) {
		t.Errorf("expected the malformed call not to enter history, got %s", (*bodies)[1])
	}
}

func TestAFailedCallIsMarkedAsOneOnTheWire(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(0, "toolu_1", "weather", `{"city":"London"}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("That did not work.")),
	)

	failing := tool.Implement(
		tool.Definition{Name: "weather", Description: "report weather in a city", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) {
		return "the city is not known", errors.New("lookup failed")
	})

	assistant := newAgent(t, server.URL, []tool.Tool{failing})

	if _, err := assistant.Send(t.Context(), "what is the weather?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results := toolResults(t, (*bodies)[1])
	if len(results) != 1 {
		t.Fatalf("expected one result, got %v", results)
	}

	if !results[0].IsError {
		t.Errorf("expected the failure to be marked, got %+v", results[0])
	}
}

func TestAThoughtHeldForARetryIsNeverMarkedForCaching(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{
				messageStart,
				thinkingStart(0),
				thinkingDelta(0, "Asking about the weather first."),
				signatureDelta(0, "seal-1"),
				blockStop(0),
			},
			toolTurn(1, "toolu_1", "weather", `{"city":"London",,}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("I corrected the call.")),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "what is the weather?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*bodies) != 2 {
		t.Fatalf("expected the malformed response to be retried once, got %d requests", len(*bodies))
	}

	retried := requestBlocks(t, (*bodies)[1])
	if !slices.ContainsFunc(retried, func(block map[string]any) bool {
		return block["type"] == "thinking"
	}) {
		t.Fatalf("expected the thought to be carried into the retry, got %s", (*bodies)[1])
	}

	for _, block := range retried {
		if _, isMarked := block["cache_control"]; isMarked {
			t.Errorf("expected the endpoint to place the breakpoint, got %v", block)
		}
	}
}

func TestARetriedRequestEndsWithAUserMessage(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{
				messageStart,
				thinkingStart(0),
				thinkingDelta(0, "Asking about the weather first."),
				signatureDelta(0, "seal-1"),
				blockStop(0),
			},
			toolTurn(1, "toolu_1", "weather", `{"city":"London",,}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("I corrected the call.")),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "what is the weather?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*bodies) != 2 {
		t.Fatalf("expected the malformed response to be retried once, got %d requests", len(*bodies))
	}

	if last := lastMessage(t, (*bodies)[1]); last.Role != "user" {
		t.Errorf("expected the retried conversation to end with a user message, got %q", last.Role)
	}

	if !strings.Contains((*bodies)[1], "did not contain a JSON object") {
		t.Errorf("expected the model to be told what was wrong, got %s", (*bodies)[1])
	}

	if strings.Contains(systemPrompt(t, (*bodies)[1]), "could not be used because") {
		t.Errorf("expected the correction to stay out of the system prompt, got %s", (*bodies)[1])
	}

	state, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}

	if held := heldConversation(state); !strings.Contains(held, "did not contain a JSON object") {
		t.Errorf("expected the correction to stay in the conversation, got %s", held)
	}
}

func systemPrompt(t *testing.T, body string) string {
	t.Helper()

	var request struct {
		System []map[string]any `json:"system"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatal(err)
	}

	written, err := json.Marshal(request.System)
	if err != nil {
		t.Fatal(err)
	}

	return string(written)
}

func requestBlocks(t *testing.T, body string) []map[string]any {
	t.Helper()

	var request struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatal(err)
	}

	var blocks []map[string]any
	for _, message := range request.Messages {
		blocks = append(blocks, message.Content...)
	}

	return blocks
}

func TestAMalformedCallRejectsTheWholeResponse(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(0, "toolu_bad", "weather", `{"city":"London",,}`),
			toolTurn(1, "toolu_good", "weather", `{"city":"Paris"}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("I corrected the first call.")),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "what is the weather?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 0 {
		t.Errorf("expected no call from the rejected response to run, ran %d times", callCount)
	}
	if strings.Contains((*bodies)[1], "toolu_bad") || strings.Contains((*bodies)[1], "toolu_good") ||
		strings.Contains((*bodies)[1], `{"city":"London",,}`) || strings.Contains((*bodies)[1], `{"city":"Paris"}`) {
		t.Errorf("expected the rejected response not to enter history, got %s", (*bodies)[1])
	}
	if results := toolResults(t, (*bodies)[1]); len(results) != 0 {
		t.Errorf("expected no result from the rejected response, got %+v", results)
	}
}

func TestAToolIsOfferedAndReadBackUnderClaudeCodesCasing(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(0, "toolu_1", "Read", `{}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("Read it.")),
	)

	assistant := newAgent(t, server.URL, []tool.Tool{emptyTool("read")})

	if _, err := assistant.Send(t.Context(), "read the file"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[0], `"name":"Read"`) {
		t.Errorf("expected the tool to be offered as Read, got %s", (*bodies)[0])
	}

	results := toolResults(t, (*bodies)[1])
	if len(results) != 1 || string(results[0].Content) != `"done"` {
		t.Errorf("expected the local tool to have run, got %+v", results)
	}
}

func TestAThoughtIsReportedWholeAndStoredWithItsSeal(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{
				messageStart,
				thinkingStart(0),
				thinkingDelta(0, "Checking "),
				thinkingDelta(0, "the sky."),
				signatureDelta(0, "seal-1"),
				blockStop(0),
			},
			[]string{textStart(1), textDelta(1, "It is raining."), blockStop(1)},
			[]string{stop("end_turn"), messageStop},
		),
		script(answer("Still raining.")),
	)

	assistant := newAgent(t, server.URL, nil)

	var reasoningDeltas strings.Builder
	var reasoning []string
	for update, err := range assistant.Stream(t.Context(), "what is the weather?", nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if update.Delta != nil && update.Delta.Kind == agent.ModelReasoningEvent {
			reasoningDeltas.WriteString(update.Delta.Text)
		}
		if update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent {
			reasoning = append(reasoning, update.Event.Text)
		}
	}

	if reasoningDeltas.String() != "Checking the sky." {
		t.Errorf("expected the thought to stream, got %q", reasoningDeltas.String())
	}
	if len(reasoning) != 1 || reasoning[0] != "Checking the sky." {
		t.Errorf("expected one whole thought, got %v", reasoning)
	}

	if _, err := assistant.Send(t.Context(), "and now?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{"type":"thinking","thinking":"Checking the sky.","signature":"seal-1"}`
	if !strings.Contains((*bodies)[1], want) {
		t.Errorf("expected %s, got %s", want, (*bodies)[1])
	}
}

func TestAnUnsealedThoughtIsDropped(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{
				messageStart,
				thinkingStart(0),
				thinkingDelta(0, "Half a thought."),
				blockStop(0),
			},
			[]string{stop("end_turn"), messageStop},
		),
		script(answer("Carrying on.")),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "think about it"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := assistant.Send(t.Context(), "and now?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains((*bodies)[1], `"type":"thinking"`) {
		t.Errorf("expected an unsealed thought not to be sent back as one, got %s", (*bodies)[1])
	}

	if strings.Contains((*bodies)[1], "Half a thought.") {
		t.Errorf("expected the unsealed thought to be dropped, got %s", (*bodies)[1])
	}
}

func TestAnUnfinishedCallIsDroppedWhenTheTurnIsAbandoned(t *testing.T) {
	server, _ := turns(
		t,
		script(
			[]string{messageStart, toolStart(0, "toolu_1", "weather")},
			[]string{argumentsDelta(0, `{"city":"Lon`)},
			[]string{textStart(1), textDelta(1, "hold on"), blockStop(1)},
			[]string{stop("end_turn"), messageStop},
		),
	)

	client := newClient(t, server.URL)
	client.Configure("", []tool.Definition{{Name: "weather", Description: "weather"}})
	client.AddUserMessage("what is the weather?")

	reply, err := client.Send(t.Context(), func(event agent.Output) bool {
		return event.Kind != agent.ModelMessageEvent
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reply.Calls) != 0 {
		t.Errorf("expected no answerable calls, got %v", reply.Calls)
	}

	for _, item := range client.Dump() {
		if strings.Contains(string(item), "tool_use") {
			t.Errorf("expected the unfinished call to be dropped, got %s", item)
		}
	}
}

func TestAnImageReturnedByAToolIsSentForTheModelToInspect(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(0, "toolu_1", "view", `{}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("I can see it.")),
	)

	viewTool := tool.Implement(
		tool.Definition{Name: "view", Description: "view an image", Schema: tool.Schema{}},
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

	results := toolResults(t, (*bodies)[1])
	if len(results) != 1 {
		t.Fatalf("expected one result, got %v", results)
	}

	var blocks []struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source"`
	}
	if err := json.Unmarshal(results[0].Content, &blocks); err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 3 {
		t.Fatalf("expected the text, attachment notice, and image, got %+v", blocks)
	}

	if blocks[0].Type != "text" || blocks[0].Text != "image/png image (3 bytes)" {
		t.Errorf("expected the tool's own text first, got %+v", blocks[0])
	}
	if blocks[1].Type != "text" || blocks[1].Text != "Image attached." {
		t.Errorf("expected an attachment notice, got %+v", blocks[1])
	}

	if blocks[2].Type != "image" || blocks[2].Source.Type != "base64" ||
		blocks[2].Source.MediaType != "image/png" || blocks[2].Source.Data != "AQID" {
		t.Errorf("expected the image to be carried as base64, got %+v", blocks[2])
	}
}

func TestAToolReturningOnlyAnImageStillSaysSomething(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart},
			toolTurn(0, "toolu_1", "view", `{}`),
			[]string{stop("tool_use"), messageStop},
		),
		script(answer("I can see it.")),
	)

	viewTool := tool.Implement(
		tool.Definition{Name: "view", Description: "view an image", Schema: tool.Schema{}},
		func(nothing) (string, string) { return "picture.png", "" },
	).Run(func(context.Context, nothing) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{
			Image: tool.Image{MediaType: "image/png", Data: []byte{1, 2, 3}},
		}, nil
	})

	assistant := newAgent(t, server.URL, []tool.Tool{viewTool})
	if _, err := assistant.Send(t.Context(), "inspect the image"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[1], `"text":"No output."`) {
		t.Errorf("expected a stand-in for the missing text, got %s", (*bodies)[1])
	}
	if !strings.Contains((*bodies)[1], `"text":"Image attached."`) {
		t.Errorf("expected an attachment notice, got %s", (*bodies)[1])
	}
}

func TestAdjacentMessagesSharingARoleAreJoinedOnTheWayOut(t *testing.T) {
	server, bodies := turns(t, script(answer("Right.")))

	client := newClient(t, server.URL)
	client.AddToolResults([]agent.ToolCallResult{{ID: "toolu_1", Output: "raining"}})
	client.AddUserMessage("and now?")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sent := requestMessages(t, (*bodies)[0])
	if len(sent) != 1 {
		t.Fatalf("expected the two user messages to be joined, got %d", len(sent))
	}

	if len(sent[0].Content) != 2 {
		t.Errorf("expected both blocks in the one message, got %v", sent[0].Content)
	}

	stored := client.Dump()
	if len(stored) != 3 {
		t.Fatalf("expected the two prompts and the answer, got %d", len(stored))
	}

	for _, item := range stored[:2] {
		if !strings.Contains(string(item), `"role":"user"`) {
			t.Errorf("expected stored state to keep the two apart, got %s", item)
		}
	}
}

func TestSendReportsAResponseCutShortAgainstTheTokenLimit(t *testing.T) {
	server, _ := turns(
		t,
		script(
			[]string{messageStart, textStart(0), textDelta(0, "It is raining ")},
			[]string{blockStop(0), stop("max_tokens"), messageStop},
		),
	)

	assistant := newAgent(t, server.URL, nil)

	reply, err := assistant.Send(t.Context(), "what is the weather?")
	if !errors.Is(err, anthropic.ErrIncomplete) {
		t.Fatalf("expected an incomplete response to be reported, got %v", err)
	}

	if reply != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", reply)
	}
}

func sendOneTurn(t *testing.T, url string, prompt string) (agent.Reply, error) {
	t.Helper()

	client := newClient(t, url)
	client.AddUserMessage(prompt)

	return client.Send(t.Context(), func(agent.Output) bool { return true })
}

func TestUsageCountsEverythingTheTurnReadAndNotOnlyWhatWasReadAfresh(t *testing.T) {
	server, _ := turns(
		t,
		script(
			[]string{startWithUsage(120, 4000, 880)},
			[]string{textStart(0), textDelta(0, "Hello."), blockStop(0)},
			[]string{stopWithUsage("end_turn", 120, 4000, 880), messageStop},
		),
	)

	reply, err := sendOneTurn(t, server.URL, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply.Usage.InputTokens != 5000 {
		t.Errorf("expected the cached tokens to be counted too, got %d", reply.Usage.InputTokens)
	}
	if reply.Usage.Cache == nil || reply.Usage.Cache.ReadTokens != 4000 || reply.Usage.Cache.WriteTokens != 880 {
		t.Errorf("expected the cache usage to be retained, got %#v", reply.Usage.Cache)
	}
}

func TestAnAccountOfOnlyWhatWasWrittenLeavesTheContextCountStanding(t *testing.T) {
	server, _ := turns(
		t,
		script(
			[]string{startWithUsage(1200, 300, 0)},
			[]string{textStart(0), textDelta(0, "Hello."), blockStop(0)},
			[]string{stop("end_turn"), messageStop},
		),
	)

	reply, err := sendOneTurn(t, server.URL, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply.Usage.InputTokens != 1500 {
		t.Errorf("expected what the turn opened with to stand, got %d", reply.Usage.InputTokens)
	}
}

func TestASealedThoughtHoldingNoTextIsStillSentBackAsItArrived(t *testing.T) {
	server, bodies := turns(
		t,
		script(
			[]string{messageStart, thinkingStart(0), signatureDelta(0, "seal-1"), blockStop(0)},
			[]string{textStart(1), textDelta(1, "It is raining."), blockStop(1)},
			[]string{stop("end_turn"), messageStop},
		),
		script(answer("Still raining.")),
	)

	client := newClient(t, server.URL)
	client.AddUserMessage("what is the weather?")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.AddUserMessage("and now?")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{"type":"thinking","thinking":"","signature":"seal-1"}`
	if !strings.Contains((*bodies)[1], want) {
		t.Errorf("expected %s, got %s", want, (*bodies)[1])
	}
}

func TestSendReportsAResponseThatRanOutOfContextWindow(t *testing.T) {
	server, _ := turns(
		t,
		script(
			[]string{messageStart, textStart(0), textDelta(0, "It is raining ")},
			[]string{blockStop(0), stop("model_context_window_exceeded"), messageStop},
		),
	)

	reply, err := newAgent(t, server.URL, nil).Send(t.Context(), "what is the weather?")
	if !errors.Is(err, anthropic.ErrIncomplete) {
		t.Fatalf("expected an incomplete response to be reported, got %v", err)
	}

	if reply != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", reply)
	}
}

func TestAPausedTurnIsTakenAsAnEndRatherThanAFailure(t *testing.T) {
	server, _ := turns(
		t,
		script(
			[]string{messageStart, textStart(0), textDelta(0, "Halfway there.")},
			[]string{blockStop(0), stop("pause_turn"), messageStop},
		),
	)

	reply, err := newAgent(t, server.URL, nil).Send(t.Context(), "take your time")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "Halfway there." {
		t.Errorf("expected what arrived to be handed back, got %q", reply)
	}
}

func TestSendRefusesAFrameItCannotRead(t *testing.T) {
	server, _ := turns(t, events(messageStart, `{"type":`))

	_, err := sendOneTurn(t, server.URL, "hello")
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("expected a malformed frame to be refused, got %v", err)
	}
}

func TestSendShowsTheEndpointsOwnFailure(t *testing.T) {
	server, _ := turns(
		t,
		events(`{"type":"error","error":{"type":"invalid_request_error","message":"Bad prompt"}}`),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "hello"); err == nil || err.Error() != "Bad prompt" {
		t.Errorf("expected the endpoint's own message, got %v", err)
	}
}

func TestAnOverloadedStreamIsAskedAgain(t *testing.T) {
	server, bodies := turns(
		t,
		events(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`),
		script(answer("Through on the second attempt.")),
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
		t.Errorf("expected the overload to be asked again once, got %d requests", len(*bodies))
	}
}

func TestSendReportsARefusalExplanation(t *testing.T) {
	const explanation = "This request was blocked as reasoning extraction."
	server, _ := turns(
		t,
		events(
			messageStart,
			`{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{`+
				`"type":"refusal","category":"reasoning_extraction","explanation":"`+explanation+`"}}}`,
			messageStop,
		),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "show your reasoning"); err == nil || err.Error() != explanation {
		t.Errorf("expected the refusal explanation, got %v", err)
	}
}

func TestSendRefusesATruncatedStream(t *testing.T) {
	server, _ := turns(t, events(messageStart, textStart(0), textDelta(0, "It is raining ")))

	client := newClient(t, server.URL)

	reply, err := sendOnce(t, client, "what is the weather?")
	if !errors.Is(err, anthropic.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be refused, got %v", err)
	}

	if reply != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", reply)
	}
}

func sendOnce(t *testing.T, client *anthropic.Client, message string) (string, error) {
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

func hangingTurns(t *testing.T, scripted ...string) (*httptest.Server, *[]string) {
	t.Helper()

	var bodies []string
	var index int

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			bodies = append(bodies, string(body))

			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(writer, scripted[index])

			isHeld := index == 0
			index++

			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}

			if isHeld {
				<-request.Context().Done()
			}
		},
	))

	t.Cleanup(server.Close)

	return server, &bodies
}

func TestACancelledTurnOrphansAThoughtTheScreenAlreadyShowed(t *testing.T) {
	server, _ := hangingTurns(t, script([]string{
		messageStart,
		thinkingStart(0),
		thinkingDelta(0, "Checking "),
		thinkingDelta(0, "the sky."),
		signatureDelta(0, "seal-1"),
		blockStop(0),
	}))

	assistant := newAgent(t, server.URL, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var shown []string

	for update, err := range assistant.Stream(ctx, "what is the weather?", nil) {
		if err != nil {
			break
		}

		if update.Event == nil {
			continue
		}

		if update.Event.Kind == agent.ModelReasoningEvent {
			shown = append(shown, update.Event.Text)
			cancel()
		}
	}

	if len(shown) != 1 || shown[0] != "Checking the sky." {
		t.Fatalf("expected the thought to have reached the screen, got %v", shown)
	}

	if held := conversationText(t, assistant); !strings.Contains(held, "Checking the sky.") {
		t.Errorf("expected the conversation to hold the thought the screen showed, got %s", held)
	}
}

func conversationText(t *testing.T, assistant *agent.Agent) string {
	t.Helper()

	conversation, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}

	return heldConversation(conversation)
}

func heldConversation(conversation []json.RawMessage) string {
	var held strings.Builder
	for _, item := range conversation {
		held.Write(item)
	}

	return held.String()
}

func TestATurnThatFailedKeepsWhatItSaidAndNotWhatItAskedFor(t *testing.T) {
	server, _ := turns(t, script(
		[]string{messageStart, textStart(0), textDelta(0, "Looking it up."), blockStop(0)},
		toolTurn(1, "call_1", "weather", `{"city":"Paris"}`),
	))

	client := newClient(t, server.URL)
	client.Configure("You are a helpful assistant", []tool.Definition{tool.Describe(emptyTool("weather"))})

	if _, err := sendOnce(t, client, "what is the weather?"); !errors.Is(err, anthropic.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be refused, got %v", err)
	}

	held := heldConversation(client.Dump())

	if !strings.Contains(held, "Looking it up.") {
		t.Errorf("expected the conversation to hold what the model said, got %s", held)
	}

	if strings.Contains(held, "tool_use") {
		t.Errorf("expected an unanswerable call to be left out, got %s", held)
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

	ctx, cancel := context.WithCancel(t.Context())

	failure := make(chan error, 1)

	go func() {
		_, err := newAgent(t, server.URL, nil).Send(ctx, "are you there")
		failure <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

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
