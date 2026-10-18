package generatecontent_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/google/generatecontent"
)

type sentRequest struct {
	path   string
	query  string
	header http.Header
	body   map[string]any
	raw    string
}

type scriptedServer struct {
	mutex    sync.Mutex
	requests []sentRequest
	replies  [][]string
}

func serve(t *testing.T, replies ...[]string) (*httptest.Server, *scriptedServer) {
	t.Helper()

	script := &scriptedServer{replies: replies}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		script.mutex.Lock()
		index := len(script.requests)
		script.requests = append(script.requests, sentRequest{
			path:   request.URL.Path,
			query:  request.URL.RawQuery,
			header: request.Header.Clone(),
			body:   body,
			raw:    string(raw),
		})
		script.mutex.Unlock()

		writer.Header().Set("Content-Type", "text/event-stream")
		if index >= len(script.replies) {
			return
		}
		for _, payload := range script.replies[index] {
			_, _ = fmt.Fprintf(writer, "data: %s\r\n\r\n", payload)
		}
	}))
	t.Cleanup(server.Close)

	return server, script
}

func (self *scriptedServer) request(t *testing.T, index int) sentRequest {
	t.Helper()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if index >= len(self.requests) {
		t.Fatalf("expected request %d, got %d requests", index+1, len(self.requests))
	}

	return self.requests[index]
}

func newClient(t *testing.T, url string, effort string) *generatecontent.Client {
	t.Helper()

	header := http.Header{}
	header.Set("X-Goog-Api-Key", "secret")

	client, err := generatecontent.New(url+"/google/v1beta", header, "gemini-9", effort, 32_000)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func collect(outputs *[]agent.Output) agent.Yield {
	return func(output agent.Output) bool {
		*outputs = append(*outputs, output)
		return true
	}
}

func ignore(agent.Output) bool {
	return true
}

const finished = `{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}],` +
	`"usageMetadata":{"promptTokenCount":1200,"cachedContentTokenCount":900,"candidatesTokenCount":40,` +
	`"thoughtsTokenCount":60,"toolUsePromptTokenCount":5}}`

func said(text string) string {
	return fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]}}]}`, text)
}

func thought(text string) string {
	return fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":%q,"thought":true}]}}]}`, text)
}

func stopped(reason string, message string) string {
	return fmt.Sprintf(`{"candidates":[{"finishReason":%q,"finishMessage":%q}],`+
		`"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1}}`, reason, message)
}

func weather() tool.Tool {
	type weatherArguments struct {
		City string `json:"city"`
	}

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

func TestNewHoldsWhatItWasAskedAndRefusesWhatIsMissing(t *testing.T) {
	client, err := generatecontent.New("http://somewhere/v1beta", nil, "gemini-9", "low", 64_000)
	if err != nil {
		t.Fatal(err)
	}
	if client.URL != "http://somewhere/v1beta" || client.Model != "gemini-9" || client.Effort != "low" || client.MaxOutputTokens != 64_000 {
		t.Errorf("expected what was asked for to be held verbatim, got %+v", client)
	}

	for _, test := range []struct {
		url             string
		model           string
		maxOutputTokens int
		want            string
	}{
		{"", "gemini-9", 64_000, "google: URL is empty"},
		{"http://somewhere", "", 64_000, "google: Model is empty"},
		{"http://somewhere", "gemini-9", 0, "google: MaxOutputTokens is 0"},
	} {
		if _, err := generatecontent.New(test.url, nil, test.model, "", test.maxOutputTokens); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("expected %q, got %v", test.want, err)
		}
	}
}

func TestATurnIsAskedForAsAStreamOfTheNamedModel(t *testing.T) {
	server, script := serve(t, []string{said("Hello."), finished})

	client := newClient(t, server.URL, "high")
	client.SetRequestHeader("X-Opencode-Session", "session-1")
	client.Configure("Be brief.", []tool.Definition{tool.Describe(weather())})
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	sent := script.request(t, 0)
	if sent.path != "/google/v1beta/models/gemini-9:streamGenerateContent" || sent.query != "alt=sse" {
		t.Errorf("got %s?%s", sent.path, sent.query)
	}
	if sent.header.Get("X-Goog-Api-Key") != "secret" || sent.header.Get("X-Opencode-Session") != "session-1" ||
		sent.header.Get("Accept") != "text/event-stream" {
		t.Errorf("expected an authorised, scoped streaming request, got %v", sent.header)
	}
	if _, isSent := sent.body["model"]; isSent {
		t.Errorf("expected the model to be named only in the address, got %v", sent.body["model"])
	}

	wantFragments := []string{
		`"systemInstruction":{"parts":[{"text":"Be brief."}]}`,
		`"contents":[{"role":"user","parts":[{"text":"hello"}]}]`,
		`"tools":[{"functionDeclarations":[{"name":"weather","description":"report weather in a city","parametersJsonSchema":{`,
		`"additionalProperties":false`,
		`"generationConfig":{"maxOutputTokens":32000,"thinkingConfig":{"includeThoughts":true,"thinkingLevel":"high"}}`,
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(sent.raw, fragment) {
			t.Errorf("expected the request to carry %s, got %s", fragment, sent.raw)
		}
	}
}

func TestAModelTakingNoEffortIsAskedForItsThoughtsAlone(t *testing.T) {
	server, script := serve(t, []string{said("Hello."), finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	sent := script.request(t, 0)
	if !strings.Contains(sent.raw, `"thinkingConfig":{"includeThoughts":true}`) {
		t.Errorf("expected thoughts and no level, got %s", sent.raw)
	}
	for _, field := range []string{"tools", "systemInstruction"} {
		if _, isSent := sent.body[field]; isSent {
			t.Errorf("expected no %s, got %v", field, sent.body[field])
		}
	}
}

func TestRequestHeadersAreOwnedByTheClient(t *testing.T) {
	header := http.Header{}
	header.Set("X-Goog-Api-Key", "secret")

	server, script := serve(t, []string{said("Hello."), finished})
	client, err := generatecontent.New(server.URL, header, "gemini-9", "", 1_000)
	if err != nil {
		t.Fatal(err)
	}
	header.Set("X-Goog-Api-Key", "changed")

	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	if got := script.request(t, 0).header.Get("X-Goog-Api-Key"); got != "secret" {
		t.Errorf("expected the key the client was given, got %q", got)
	}
}

func TestThoughtsAndAnswerAreStreamedApartAndClosedWithTheirUsage(t *testing.T) {
	server, _ := serve(t, []string{thought("Weighing "), thought("it up."), said("The "), said("answer."), finished})

	client := newClient(t, server.URL, "high")
	client.AddUserMessage("hello")

	var outputs []agent.Output
	reply, err := client.Send(t.Context(), collect(&outputs))
	if err != nil {
		t.Fatal(err)
	}

	want := agent.Usage{InputTokens: 1205, OutputTokens: 100, Cache: &agent.CacheUsage{ReadTokens: 900}}
	if reply.Usage.InputTokens != want.InputTokens || reply.Usage.OutputTokens != want.OutputTokens ||
		reply.Usage.Cache == nil || *reply.Usage.Cache != *want.Cache {
		t.Errorf("got usage %+v, want %+v", reply.Usage, want)
	}

	var shape []string
	for _, output := range outputs {
		shape = append(shape, fmt.Sprintf("%s:%q:%t:%t", output.Kind, output.Text, output.Done, output.Usage != nil))
	}
	wantShape := []string{
		`model_reasoning:"Weighing ":false:false`,
		`model_reasoning:"it up.":false:false`,
		`model_reasoning:"":true:false`,
		`model_message:"The ":false:false`,
		`model_message:"answer.":false:false`,
		`model_message:"":true:true`,
	}
	if strings.Join(shape, "\n") != strings.Join(wantShape, "\n") {
		t.Errorf("got outputs\n%s\nwant\n%s", strings.Join(shape, "\n"), strings.Join(wantShape, "\n"))
	}

	stored := string(client.Dump()[1])
	if stored != `{"role":"model","parts":[{"text":"Weighing it up.","thought":true},{"text":"The answer."}]}` {
		t.Errorf("expected the deltas to be gathered into one part of each kind, got %s", stored)
	}
}

func TestASignedPartIsKeptApartFromTheTextAroundIt(t *testing.T) {
	server, script := serve(t,
		[]string{
			said("Part "),
			said("one."),
			`{"candidates":[{"content":{"role":"model","parts":[{"text":" Part two.","thoughtSignature":"seal-1"}]}}]}`,
			said(" Part "),
			said("three."),
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"","thoughtSignature":"seal-2"}]},"finishReason":"STOP"}]}`,
		},
		[]string{said("Again."), finished},
	)

	client := newClient(t, server.URL, "")
	client.AddUserMessage("hello")

	var outputs []agent.Output
	if _, err := client.Send(t.Context(), collect(&outputs)); err != nil {
		t.Fatal(err)
	}

	var answer strings.Builder
	for _, output := range outputs {
		answer.WriteString(output.Text)
	}
	if answer.String() != "Part one. Part two. Part three." {
		t.Errorf("expected the whole answer to be streamed, got %q", answer.String())
	}

	want := `{"role":"model","parts":[{"text":"Part one."},{"text":" Part two.","thoughtSignature":"seal-1"},` +
		`{"text":" Part three."},{"text":"","thoughtSignature":"seal-2"}]}`
	if stored := string(client.Dump()[1]); stored != want {
		t.Errorf("expected each signed part on its own, got %s", stored)
	}

	client.AddUserMessage("again")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script.request(t, 1).raw, want) {
		t.Errorf("expected the signed parts to be sent back where they stood, got %s", script.request(t, 1).raw)
	}
}

const signedCall = `{"candidates":[{"content":{"role":"model","parts":[` +
	`{"functionCall":{"id":"fc-1","name":"weather","args":{"city": "Leeds"}},"thoughtSignature":"seal-2"},` +
	`{"functionCall":{"name":"weather","args":{}}}` +
	`]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`

func TestCallsAreHandedOverAndAnsweredWhereTheModelAskedThem(t *testing.T) {
	server, script := serve(t, []string{thought("Two cities."), signedCall}, []string{said("Wet."), finished})

	client := newClient(t, server.URL, "high")
	client.Configure("", []tool.Definition{tool.Describe(weather())})
	client.AddUserMessage("weather?")

	var outputs []agent.Output
	reply, err := client.Send(t.Context(), collect(&outputs))
	if err != nil {
		t.Fatal(err)
	}

	want := []agent.ToolCall{
		{ID: "fc-1", Name: "weather", Arguments: `{"city":"Leeds"}`},
		{ID: "call_1_2", Name: "weather", Arguments: `{}`},
	}
	if fmt.Sprint(reply.Calls) != fmt.Sprint(want) {
		t.Errorf("got calls %+v, want %+v", reply.Calls, want)
	}
	if len(outputs) < 2 || !outputs[1].Done || outputs[1].Kind != agent.ModelReasoningEvent {
		t.Errorf("expected the thought to be closed before the calls, got %+v", outputs)
	}

	restored := newClient(t, server.URL, "high")
	restored.Load(client.Dump())
	restored.AddToolResults([]agent.ToolCallResult{
		{ID: "fc-1", Output: "raining"},
		{ID: "call_1_2", Output: "no such city", IsError: true},
	})
	restored.AddUserMessage("<system-reminder>\na job ended\n</system-reminder>")
	if _, err := restored.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	sent := script.request(t, 1).raw
	wantContents := `"contents":[` +
		`{"role":"user","parts":[{"text":"weather?"}]},` +
		`{"role":"model","parts":[{"text":"Two cities.","thought":true},` +
		`{"functionCall":{"id":"fc-1","name":"weather","args":{"city":"Leeds"}},"thoughtSignature":"seal-2"},` +
		`{"functionCall":{"name":"weather","args":{}}}]},` +
		`{"role":"user","parts":[` +
		`{"functionResponse":{"id":"fc-1","name":"weather","response":{"output":"raining"}}},` +
		`{"functionResponse":{"name":"weather","response":{"error":"no such city\n\n\u003csystem-reminder\u003e\na job ended\n\u003c/system-reminder\u003e"}}}]}]`
	if !strings.Contains(sent, wantContents) {
		t.Errorf("expected the calls verbatim, and their answers in one turn carrying what followed them, got %s", sent)
	}

	if len(restored.Dump()) != 5 {
		t.Errorf("expected the stored turns to stay apart, got %d of them", len(restored.Dump()))
	}
}

func TestWhatFollowsAToolResultIsCarriedInsideItsAnswer(t *testing.T) {
	server, script := serve(t, []string{signedCall}, []string{said("Noted."), finished}, []string{said("Bye."), finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("go")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}
	client.AddToolResults([]agent.ToolCallResult{{ID: "fc-1", Output: "first"}, {ID: "call_1_1", Output: "second"}})
	client.AddUserMessage("one")
	client.AddUserMessage("two")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}
	client.AddUserMessage("later")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	first := script.request(t, 1).raw
	want := `{"role":"user","parts":[{"functionResponse":{"id":"fc-1","name":"weather","response":{"output":"first"}}},` +
		`{"functionResponse":{"name":"weather","response":{"output":"second\n\none\n\ntwo"}}}]}]`
	if !strings.Contains(first, want) {
		t.Errorf("expected the following messages inside the last answer and no text beside it, got %s", first)
	}
	if second := script.request(t, 2).raw; !strings.Contains(second, strings.TrimSuffix(want, "]")+`,{"role":"model"`) ||
		!strings.Contains(second, `{"role":"user","parts":[{"text":"later"}]}`) {
		t.Errorf("expected the folded turn unchanged and a later message on its own, got %s", second)
	}

	if stored := client.Dump(); len(stored) != 8 || string(stored[3]) != `{"role":"user","parts":[{"text":"one"}]}` {
		t.Errorf("expected the stored history to keep every message apart, got %s", stored)
	}
}

func TestAToolThatSaysNothingSaysSo(t *testing.T) {
	server, script := serve(t, []string{signedCall}, []string{said("Done."), finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("go")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}
	client.AddToolResults([]agent.ToolCallResult{{ID: "fc-1"}, {ID: "call_1_1"}})
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(script.request(t, 1).raw, `"response":{"output":"No output."}`) {
		t.Errorf("expected an empty result to say so, got %s", script.request(t, 1).raw)
	}
}

func gifOf(t *testing.T) []byte {
	t.Helper()

	subject := image.NewPaletted(image.Rect(0, 0, 4, 3), color.Palette{color.Black, color.White})
	var encoded bytes.Buffer
	if err := gif.Encode(&encoded, subject, nil); err != nil {
		t.Fatal(err)
	}

	return encoded.Bytes()
}

func TestAnImageFollowsItsResultAndIsCarriedInAFormTheEndpointReads(t *testing.T) {
	server, script := serve(t, []string{signedCall}, []string{said("Seen."), finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("look")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	picture := gifOf(t)
	client.AddToolResults([]agent.ToolCallResult{
		{ID: "fc-1", Output: "a picture", Image: tool.Image{MediaType: "image/gif", Data: picture}},
		{ID: "call_1_1", Output: "half a picture", Image: tool.Image{MediaType: "image/png"}},
	})
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Contents []struct {
			Parts []struct {
				FunctionResponse *struct {
					Parts []struct {
						InlineData struct {
							MediaType string `json:"mimeType"`
							Data      string `json:"data"`
						} `json:"inlineData"`
					} `json:"parts"`
				} `json:"functionResponse"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(script.request(t, 1).raw), &body); err != nil {
		t.Fatal(err)
	}

	answers := body.Contents[len(body.Contents)-1].Parts
	if len(answers) != 2 || answers[0].FunctionResponse == nil || len(answers[0].FunctionResponse.Parts) != 1 {
		t.Fatalf("expected the picture inside the first answer, got %s", script.request(t, 1).raw)
	}
	if len(answers[1].FunctionResponse.Parts) != 0 {
		t.Errorf("expected a picture without data to be left out, got %+v", answers[1].FunctionResponse.Parts)
	}

	carried := answers[0].FunctionResponse.Parts[0].InlineData
	data, err := base64.StdEncoding.DecodeString(carried.Data)
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); carried.MediaType != "image/png" || err != nil || format != "png" {
		t.Errorf("expected a GIF to be carried as a PNG, got %s (%s, %v)", carried.MediaType, format, err)
	}

	if stored := string(client.Dump()[2]); !strings.Contains(stored, base64.StdEncoding.EncodeToString(picture)) {
		t.Errorf("expected the stored history to keep the picture as it came, got %s", stored)
	}
}

func TestAnAnswerCutShortIsReportedAndWhatWasSaidIsKept(t *testing.T) {
	server, script := serve(t, []string{said("Half"), stopped("MAX_TOKENS", "")}, []string{said("Rest."), finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); !errors.Is(err, generatecontent.ErrIncomplete) {
		t.Fatalf("expected the answer to be reported cut short, got %v", err)
	}

	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	sent := script.request(t, 1).raw
	if !strings.Contains(sent, `{"role":"model","parts":[{"text":"Half"}]},{"role":"user","parts":[{"text":"Your previous response was interrupted. Continue from where you left off."}]}`) {
		t.Errorf("expected what was said and a request to carry on, got %s", sent)
	}
}

func TestAMalformedCallIsCorrectedInTheConversationAndAskedAgain(t *testing.T) {
	server, script := serve(t,
		[]string{stopped("MALFORMED_FUNCTION_CALL", "Malformed function call: weather(city=)")},
		[]string{said("Fixed."), finished},
	)

	client := newClient(t, server.URL, "")
	client.AddUserMessage("weather?")

	_, err := client.Send(t.Context(), ignore)
	var retriable agent.Retriable
	var resumable agent.Resumable
	if !errors.As(err, &retriable) || !retriable.Retriable() || !errors.As(err, &resumable) || !resumable.Resumable() {
		t.Fatalf("expected a malformed call to be asked again where it stood, got %v", err)
	}
	if !strings.Contains(err.Error(), "weather(city=)") {
		t.Errorf("expected the failure to say what was malformed, got %v", err)
	}

	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	want := `{"role":"user","parts":[{"text":"weather?"},{"text":"Your previous response could not be used because its function call was malformed (Malformed function call: weather(city=)). Try again with a well-formed function call."}]}`
	if sent := script.request(t, 1).raw; !strings.Contains(sent, want) {
		t.Errorf("expected the correction beside the question, got %s", sent)
	}
}

func TestEveryOtherStopNamesItsReason(t *testing.T) {
	for _, test := range []struct {
		replies []string
		want    string
	}{
		{[]string{said("Well"), stopped("SAFETY", "")}, "the endpoint stopped the response for SAFETY"},
		{[]string{stopped("RECITATION", "too familiar")}, "the endpoint stopped the response for RECITATION: too familiar"},
		{[]string{`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`}, "the endpoint refused the prompt for PROHIBITED_CONTENT"},
		{[]string{`{"error":{"code":503,"message":"The model is overloaded.","status":"UNAVAILABLE"}}`}, "The model is overloaded."},
		{[]string{`{"error":{"code":503,"status":"UNAVAILABLE"}}`}, "UNAVAILABLE"},
		{[]string{`not json`}, "the endpoint sent something unreadable: not json"},
	} {
		server, _ := serve(t, test.replies)

		client := newClient(t, server.URL, "")
		client.AddUserMessage("hello")
		if _, err := client.Send(t.Context(), ignore); err == nil || err.Error() != test.want {
			t.Errorf("expected %q, got %v", test.want, err)
		}
	}
}

func TestAStreamThatEndsWithoutAReasonIsTruncated(t *testing.T) {
	server, _ := serve(t, []string{said("Half"), said(" of it")})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("hello")

	_, err := client.Send(t.Context(), ignore)
	if !errors.Is(err, generatecontent.ErrTruncated) {
		t.Fatalf("expected a truncated stream, got %v", err)
	}
	if stored := client.Dump(); len(stored) != 2 || string(stored[1]) != `{"role":"model","parts":[{"text":"Half of it"}]}` {
		t.Errorf("expected what was said to be kept, got %s", stored)
	}
}

func TestAFailedTurnKeepsWhatItSaidAndNotWhatItAskedFor(t *testing.T) {
	server, _ := serve(t, []string{said("Let me look."), strings.Replace(signedCall, `"finishReason":"STOP"`, `"finishReason":"SAFETY"`, 1)})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); err == nil {
		t.Fatal("expected the stop to be reported")
	}

	if stored := client.Dump(); len(stored) != 2 || string(stored[1]) != `{"role":"model","parts":[{"text":"Let me look."}]}` {
		t.Errorf("expected only what was said to be kept, got %s", stored)
	}
}

func TestATurnThatProducedNothingIsNotStored(t *testing.T) {
	server, _ := serve(t, []string{finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("hello")
	reply, err := client.Send(t.Context(), ignore)
	if err != nil {
		t.Fatal(err)
	}

	if len(client.Dump()) != 1 || reply.Usage.InputTokens != 1205 {
		t.Errorf("expected nothing stored and the usage reported, got %d turns and %+v", len(client.Dump()), reply.Usage)
	}
}

func TestEveryStreamEventCanStopDelivery(t *testing.T) {
	replies := []string{thought("Hmm."), said("Yes."), signedCall}

	for stopAt := range 4 {
		server, _ := serve(t, replies)

		client := newClient(t, server.URL, "")
		client.AddUserMessage("hello")

		count := 0
		_, err := client.Send(t.Context(), func(agent.Output) bool {
			count++
			return count <= stopAt
		})
		if err != nil {
			t.Errorf("stopping after %d: expected no failure, got %v", stopAt, err)
		}
		if count != stopAt+1 {
			t.Errorf("stopping after %d: expected delivery to stop, got %d outputs", stopAt, count)
		}
	}
}

func TestToolsSizeIsTheNumberOfBytesSentOnTheWire(t *testing.T) {
	server, script := serve(t, []string{said("Hi."), finished})

	client := newClient(t, server.URL, "")
	client.Configure("", []tool.Definition{tool.Describe(weather())})
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal([]byte(script.request(t, 0).raw), &body); err != nil {
		t.Fatal(err)
	}

	if got := generatecontent.ToolsSize([]tool.Tool{weather()}); got != len(body.Tools) {
		t.Errorf("got %d, want %d", got, len(body.Tools))
	}
}

func TestAToolWithNoArgumentsIsStillGivenASchema(t *testing.T) {
	server, script := serve(t, []string{said("Hi."), finished})

	client := newClient(t, server.URL, "")
	client.Configure("", []tool.Definition{{Name: "now", Description: "tell the time"}})
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	if sent := script.request(t, 0).raw; !strings.Contains(sent, `{"name":"now","description":"tell the time","parametersJsonSchema":{"type":"object","properties":{}`) {
		t.Errorf("expected an empty object schema, got %s", sent)
	}
}

func TestDumpAndLoadOwnTheirHistory(t *testing.T) {
	client := newClient(t, "http://somewhere", "")
	client.AddUserMessage("hello")

	dumped := client.Dump()
	dumped[0] = json.RawMessage(`{}`)
	if string(client.Dump()[0]) == `{}` {
		t.Error("expected a dump to be a copy")
	}

	loaded := []json.RawMessage{json.RawMessage(`{"role":"user","parts":[{"text":"stored"}]}`)}
	client.Load(loaded)
	loaded[0] = json.RawMessage(`{}`)
	if string(client.Dump()[0]) != `{"role":"user","parts":[{"text":"stored"}]}` {
		t.Errorf("expected a load to be a copy, got %s", client.Dump()[0])
	}
}

func TestTheEndpointListsNothingOfItsOwn(t *testing.T) {
	client := newClient(t, "http://somewhere", "")
	if _, err := client.Models(t.Context()); !errors.Is(err, agent.ErrNoListing) {
		t.Errorf("expected no listing, got %v", err)
	}
}

func TestAPDFIsAnsweredWithItsResultWhileAudioFollowsTheAnswers(t *testing.T) {
	server, script := serve(t, []string{signedCall}, []string{said("Heard."), finished})

	client := newClient(t, server.URL, "")
	client.AddUserMessage("listen")
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	client.AddToolResults([]agent.ToolCallResult{
		{ID: "fc-1", Output: "application/pdf, 3B", Attachment: tool.Attachment{
			Name: "paper.pdf", MediaType: "application/pdf", Data: []byte{1, 2, 3},
		}},
		{ID: "call_1_1", Output: "audio/mp3, 3B", Attachment: tool.Attachment{
			Name: "talk.mp3", MediaType: "audio/mp3", Data: []byte{4, 5, 6},
		}},
	})
	if _, err := client.Send(t.Context(), ignore); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Contents []struct {
			Parts []json.RawMessage `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(script.request(t, 1).raw), &body); err != nil {
		t.Fatal(err)
	}

	answers := body.Contents[len(body.Contents)-1].Parts
	if len(answers) != 3 {
		t.Fatalf("expected two answers and the audio after them, got %s", script.request(t, 1).raw)
	}
	for index, want := range []string{
		`"parts":[{"inlineData":{"mimeType":"application/pdf","data":"AQID"}}]`,
		`"output":"audio/mp3, 3B\n\nAudio attached."`,
		`{"inlineData":{"mimeType":"audio/mp3","data":"BAUG"}}`,
	} {
		if !strings.Contains(string(answers[index]), want) {
			t.Errorf("part %d: expected %s, got %s", index, want, answers[index])
		}
	}
}
