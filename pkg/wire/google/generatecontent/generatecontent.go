package generatecontent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

const (
	responseHeaderTimeout = 10 * time.Minute
	streamIdleTimeout     = 10 * time.Minute

	modelsPath   = "/models/"
	streamMethod = ":streamGenerateContent?alt=sse"
)

const (
	userRole  = "user"
	modelRole = "model"

	continueInstruction = "Your previous response was interrupted. Continue from where you left off."
	emptyOutputNotice   = "No output."
)

type Client struct {
	URL             string
	Model           string
	Effort          string
	MaxOutputTokens int

	requestHeader  http.Header
	instructions   string
	tools          []functionDeclaration
	history        []json.RawMessage
	requestHistory imageHistory
	requests       *req.Client
}

func New(
	url string,
	requestHeader http.Header,
	model string,
	effort string,
	maxOutputTokens int,
) (*Client, error) {
	client := &Client{
		URL:             url,
		Model:           model,
		Effort:          effort,
		MaxOutputTokens: maxOutputTokens,
		requestHeader:   cloneHeader(requestHeader),
		requests:        req.NewStreaming(responseHeaderTimeout, streamIdleTimeout),
	}

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return http.Header{}
	}

	return header.Clone()
}

func (self *Client) SetRequestHeader(name string, value string) {
	self.requestHeader.Set(name, value)
}

func (self *Client) IdleAfter(after time.Duration) {
	self.requests.IdleAfter(after)
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.requests.Observe(observer)
}

func (self *Client) Models(context.Context) ([]agent.Model, error) {
	return nil, agent.ErrNoListing
}

func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
}

func (self *Client) AddUserMessage(text string) {
	self.addUserText(text)
}

func (self *Client) AddToolResults(results []agent.ToolCallResult) {
	parts := make([]json.RawMessage, 0, len(results))

	for _, result := range results {
		call := self.callNamed(result.ID)
		parts = append(parts, encode(functionResponsePart{FunctionResponse: functionResponse{
			ID:       call.wireID,
			Name:     call.name,
			Response: responseOf(result),
			Parts:    imageOf(result),
		}}))
	}

	self.history = append(self.history, encode(content{Role: userRole, Parts: parts}))
}

func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

func (self *Client) Load(items []json.RawMessage) {
	self.history = slices.Clone(items)
	self.requestHistory.reset()
}

func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	if endsWithModelTurn(self.history) {
		self.addUserText(continueInstruction)
	}

	stream, _, err := self.requests.Stream(ctx, self.address(), self.requestBody(), self.headers())
	if err != nil {
		return agent.Reply{}, err
	}
	defer func() { _ = stream.Close() }()

	reply, err := readReply(stream, yield)
	if err != nil {
		if prose := reply.prose(); prose != nil {
			self.history = append(self.history, prose)
		}
		if malformedCall, isMalformed := errors.AsType[malformedCallError](err); isMalformed {
			self.addUserText(malformedCall.correction())
		}

		return agent.Reply{}, err
	}

	answer := reply.content()
	if answer == nil {
		return agent.Reply{Usage: reply.usage}, nil
	}

	index := len(self.history)
	self.history = append(self.history, answer)

	return agent.Reply{Calls: toolCalls(callsIn(answer, index)), Usage: reply.usage}, nil
}

func (self *Client) settled() error {
	for _, setting := range []struct {
		name  string
		value string
	}{
		{"URL", self.URL},
		{"Model", self.Model},
	} {
		if setting.value == "" {
			return fmt.Errorf("google: %s is empty", setting.name)
		}
	}

	if self.MaxOutputTokens <= 0 {
		return fmt.Errorf("google: MaxOutputTokens is %d, and must be above zero", self.MaxOutputTokens)
	}

	return nil
}

func (self *Client) address() string {
	return strings.TrimSuffix(self.URL, "/") + modelsPath + url.PathEscape(self.Model) + streamMethod
}

func (self *Client) headers() http.Header {
	header := self.requestHeader.Clone()
	header.Set("Accept", "text/event-stream")
	return header
}

func (self *Client) addUserText(text string) {
	self.history = append(self.history, encode(content{Role: userRole, Parts: []json.RawMessage{encode(textPart{Text: text})}}))
}

func (self *Client) callNamed(id string) call {
	for index, item := range slices.Backward(self.history) {
		for _, candidate := range callsIn(item, index) {
			if candidate.id == id {
				return candidate
			}
		}
	}

	return call{id: id, wireID: id}
}

func (self *Client) requestBody() request {
	body := request{
		Contents: merged(self.requestHistory.prepare(self.history)),
		GenerationConfig: generationConfig{
			MaxOutputTokens: self.MaxOutputTokens,
			ThinkingConfig:  thinkingConfig{IncludeThoughts: true, ThinkingLevel: self.Effort},
		},
	}

	if self.instructions != "" {
		body.SystemInstruction = &systemInstruction{Parts: []textPart{{Text: self.instructions}}}
	}

	if len(self.tools) > 0 {
		body.Tools = []toolGroup{{FunctionDeclarations: self.tools}}
	}

	return body
}

func encode(value any) json.RawMessage {
	encodedValue, _ := json.Marshal(value) //nolint:errchkjson // the wire values are plain structs
	return encodedValue
}

type request struct {
	Contents          []content          `json:"contents"`
	SystemInstruction *systemInstruction `json:"systemInstruction,omitempty"`
	Tools             []toolGroup        `json:"tools,omitempty"`
	GenerationConfig  generationConfig   `json:"generationConfig"`
}

type content struct {
	Role  string            `json:"role"`
	Parts []json.RawMessage `json:"parts"`
}

type systemInstruction struct {
	Parts []textPart `json:"parts"`
}

type textPart struct {
	Text             string `json:"text"`
	IsThought        bool   `json:"thought,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

type generationConfig struct {
	MaxOutputTokens int            `json:"maxOutputTokens"`
	ThinkingConfig  thinkingConfig `json:"thinkingConfig"`
}

type thinkingConfig struct {
	IncludeThoughts bool   `json:"includeThoughts"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type functionResponsePart struct {
	FunctionResponse functionResponse `json:"functionResponse"`
}

type functionResponse struct {
	ID       string           `json:"id,omitempty"`
	Name     string           `json:"name"`
	Response responseBody     `json:"response"`
	Parts    []inlineDataPart `json:"parts,omitempty"`
}

type responseBody struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

type inlineDataPart struct {
	InlineData blob `json:"inlineData"`
}

type blob struct {
	MediaType string `json:"mimeType"`
	Data      string `json:"data"`
}

func responseOf(result agent.ToolCallResult) responseBody {
	text := result.Output
	if text == "" {
		text = emptyOutputNotice
	}

	if result.IsError {
		return responseBody{Error: text}
	}

	return responseBody{Output: text}
}

func imageOf(result agent.ToolCallResult) []inlineDataPart {
	if result.Image.MediaType == "" || len(result.Image.Data) == 0 {
		return nil
	}

	return []inlineDataPart{{InlineData: blob{
		MediaType: result.Image.MediaType,
		Data:      base64.StdEncoding.EncodeToString(result.Image.Data),
	}}}
}
