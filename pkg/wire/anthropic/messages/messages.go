package messages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"crdx.org/io/internal/prefixwatch"
	"crdx.org/io/internal/req"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
)

const (
	Endpoint = "https://api.anthropic.com/v1/messages"

	Version = "2023-06-01"

	Beta = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"

	UserAgent = "claude-cli/2.1.267"

	Identity = "You are Claude Code, Anthropic's official CLI for Claude."
)

var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

const (
	responseHeaderTimeout     = 10 * time.Minute
	streamIdleTimeout         = 10 * time.Minute
	asideTimeout              = 30 * time.Second
	toolInputCorrectionFormat = "Your previous response could not be used because the %q tool call did not contain a JSON object. Try again with a JSON object for its input."
)

type TokenSource interface {
	Token() (string, error)
}

type observedTokenSource interface {
	ObserveHTTP(observer req.Observer)
}

type Client struct {
	URL             string
	Model           string
	Effort          string
	MaxOutputTokens int

	tokens         TokenSource
	instructions   string
	tools          []functionTool
	toolNames      []string
	history        []json.RawMessage
	requestHistory imageHistory
	prefix         prefixwatch.Watcher
	rewrite        string
	requests       *req.Client
	observer       req.Observer
}

func New(tokens TokenSource, model string, effort string, maxOutputTokens int) (*Client, error) {
	client := &Client{
		URL:             Endpoint,
		Model:           model,
		Effort:          effort,
		MaxOutputTokens: maxOutputTokens,
		tokens:          tokens,
		requests:        req.NewStreaming(responseHeaderTimeout, streamIdleTimeout),
	}

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

func (self *Client) IdleAfter(after time.Duration) {
	self.requests.IdleAfter(after)
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.ObserveHTTP(observer)
	}
}

func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)

	self.toolNames = make([]string, len(tools))
	for i, offer := range tools {
		self.toolNames[i] = offer.Name
	}
}

func (self *Client) AddUserMessage(text string) {
	self.addUserText(text)
}

func (self *Client) AddToolResults(results []agent.ToolCallResult) {
	blocks := make([]json.RawMessage, 0, len(results))

	for _, result := range results {
		blocks = append(blocks, encodeItem(toolResult{
			Type:      "tool_result",
			ToolUseID: result.ID,
			Content:   encodeToolOutput(result),
			IsError:   result.IsError,
		}))
	}

	self.history = append(self.history, encodeItem(message{Role: userRole, Content: blocks}))
}

func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

func (self *Client) Load(items []json.RawMessage) {
	self.history = slices.Clone(items)
	self.requestHistory.reset()
}

func encodeItem(item any) json.RawMessage {
	encodedItem, err := json.Marshal(item)
	if err != nil {
		panic(fmt.Errorf("anthropic: encode history item: %w", err))
	}

	return encodedItem
}

type invalidToolInputError struct {
	toolID    string
	toolName  string
	arguments string
}

func (self invalidToolInputError) Error() string {
	return fmt.Sprintf("the %s tool call did not contain a JSON object", self.toolName)
}

func (self invalidToolInputError) FaultedCall() agent.ToolCall {
	return agent.ToolCall{ID: self.toolID, Name: self.toolName, Arguments: self.arguments}
}

func (invalidToolInputError) Retriable() bool {
	return true
}

func (invalidToolInputError) RetryAfter() time.Duration {
	return 0
}

func (invalidToolInputError) Resumable() bool {
	return true
}

func (self invalidToolInputError) getCorrection() string {
	return fmt.Sprintf(toolInputCorrectionFormat, self.toolName)
}

func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	self.resumeInterruptedTurn()

	reply, err := self.post(ctx, yield)
	if err == nil {
		err = reply.validateToolInputs()
	}
	if err != nil {
		if prose := reply.prose(); prose != nil {
			self.history = append(self.history, prose)
		}
		if invalidInput, ok := errors.AsType[invalidToolInputError](err); ok {
			self.addUserText(invalidInput.getCorrection())
		}

		return agent.Reply{}, err
	}

	if answer := reply.message(); answer != nil {
		self.history = append(self.history, answer)
	}

	return agent.Reply{
		Calls:         reply.calls(self.toolNames),
		Usage:         reply.usage,
		PrefixRewrite: self.rewrite,
	}, nil
}

func (self *Client) observedRequests() *req.Client {
	client := req.New(asideTimeout)
	if self.observer != nil {
		client.Observe(self.observer)
	}

	return client
}

func (self *Client) settled() error {
	for _, setting := range []struct {
		name  string
		value string
	}{
		{"URL", self.URL},
		{"Model", self.Model},
		{"Effort", self.Effort},
	} {
		if setting.value == "" {
			return fmt.Errorf("anthropic: %s is empty", setting.name)
		}
	}

	if self.MaxOutputTokens <= 0 {
		return fmt.Errorf("anthropic: MaxOutputTokens is %d, and must be above zero", self.MaxOutputTokens)
	}

	if !slices.Contains(Efforts, self.Effort) {
		return fmt.Errorf(
			"anthropic: Effort is %q, and must be one of: %s",
			self.Effort, strings.Join(Efforts, ", "),
		)
	}

	return nil
}

func (self *Client) post(ctx context.Context, yield agent.Yield) (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	stream, responseHeader, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers(token))
	if err != nil {
		if isUsageLimited(responseHeader) {
			return reply{}, &agent.UsageLimitError{Cause: err, Windows: responseUsageWindows(responseHeader)}
		}

		return reply{}, err
	}
	defer func() { _ = stream.Close() }()

	return readReply(stream, yield)
}

func (self *Client) addUserText(text string) {
	self.history = append(self.history, encodeItem(message{
		Role:    userRole,
		Content: []json.RawMessage{encodeItem(textBlock{Type: "text", Text: text})},
	}))
}

func (self *Client) resumeInterruptedTurn() {
	if endsWithAssistantTurn(self.history) {
		self.addUserText(continueInstruction)
	}
}

func (self *Client) requestBody() request {
	body := self.body()
	self.rewrite = self.prefix.Look(body.Tools, body.System, body.Messages)

	return body
}

func (self *Client) body() request {
	return request{
		Model:           self.Model,
		MaxOutputTokens: self.MaxOutputTokens,
		Stream:          true,
		Cache:           ephemeral(),
		System:          self.system(),
		Tools:           self.tools,
		Thinking:        thinking{Type: "adaptive", Display: "summarized"},
		Output:          outputConfig{Effort: self.Effort},
		Messages:        encodeMessages(merged(self.requestHistory.prepare(self.history))),
	}
}

func (self *Client) system() []textBlock {
	blocks := []textBlock{{Type: "text", Text: Identity, Cache: ephemeral()}}

	if self.instructions != "" {
		blocks = append(blocks, textBlock{Type: "text", Text: self.instructions, Cache: ephemeral()})
	}

	return blocks
}

func (self *Client) headers(token string) http.Header {
	header := http.Header{}

	header.Set("Authorization", "Bearer "+token)
	header.Set("Anthropic-Version", Version)
	header.Set("Anthropic-Beta", Beta)
	header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	header.Set("Accept", "text/event-stream")
	header.Set("User-Agent", UserAgent)
	header.Set("X-App", "cli")

	return header
}

type request struct {
	Model           string            `json:"model"`
	MaxOutputTokens int               `json:"max_tokens"`
	Stream          bool              `json:"stream"`
	Cache           *cacheControl     `json:"cache_control,omitempty"`
	System          []textBlock       `json:"system,omitempty"`
	Tools           []functionTool    `json:"tools,omitempty"`
	Messages        []json.RawMessage `json:"messages"`
	Thinking        thinking          `json:"thinking"`
	Output          outputConfig      `json:"output_config"`
}

type thinking struct {
	Type    string `json:"type"`
	Display string `json:"display"`
}

type outputConfig struct {
	Effort string `json:"effort"`
}

type message struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type textBlock struct {
	Type  string        `json:"type"`
	Text  string        `json:"text"`
	Cache *cacheControl `json:"cache_control,omitempty"`
}

type toolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type imageBlock struct {
	Type   string      `json:"type"`
	Source imageSource `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

const (
	attachmentNotice  = "Image attached."
	emptyOutputNotice = "No output."
)

func encodeToolOutput(result agent.ToolCallResult) any {
	text := result.Output
	if text == "" {
		text = emptyOutputNotice
	}
	if result.Image.MediaType == "" || len(result.Image.Data) == 0 {
		return text
	}

	return []any{
		textBlock{Type: "text", Text: text},
		textBlock{Type: "text", Text: attachmentNotice},
		imageBlock{
			Type: "image",
			Source: imageSource{
				Type:      "base64",
				MediaType: result.Image.MediaType,
				Data:      base64.StdEncoding.EncodeToString(result.Image.Data),
			},
		},
	}
}
