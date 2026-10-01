package responses

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/openai/internal/imagehistory"
)

const (
	Endpoint        = "https://chatgpt.com/backend-api/codex/responses"
	Summary         = "auto"
	fastServiceTier = "priority"

	routingHintHeader = "X-Codex-Routing-Hint"

	Originator = "io"
)

var Efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

const (
	responseHeaderTimeout = 10 * time.Minute
	streamIdleTimeout     = 10 * time.Minute
	asideTimeout          = 30 * time.Second
)

type Token struct {
	Access    string
	AccountID string
}

type TokenSource interface {
	Token() (Token, error)
}

type observedTokenSource interface {
	ObserveHTTP(observer req.Observer)
}

type Client struct {
	URL    string
	Model  string
	Effort string
	IsFast bool

	MaxOutputTokens int

	tokens           TokenSource
	headerSource     func(Token) http.Header
	isEffortRequired bool
	extraHeader      http.Header
	instructions     string
	tools            []functionTool
	session          string
	history          []json.RawMessage
	requestHistory   imagehistory.Cache
	requests         *req.Client
	observer         req.Observer

	usageMutex   sync.Mutex
	usageWindows []agent.UsageWindow
}

func New(tokens TokenSource, model string, effort string) (*Client, error) {
	client := &Client{
		URL:      Endpoint,
		Model:    model,
		Effort:   effort,
		tokens:   tokens,
		session:  newToken(),
		requests: req.NewStreaming(responseHeaderTimeout, streamIdleTimeout),
	}
	client.headerSource = client.codexHeaders
	client.isEffortRequired = true

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

func NewAt(
	url string,
	requestHeader http.Header,
	model string,
	effort string,
	maxOutputTokens int,
) (*Client, error) {
	if maxOutputTokens <= 0 {
		return nil, fmt.Errorf("responses: MaxOutputTokens is %d, and must be above zero", maxOutputTokens)
	}

	fixedHeader := requestHeader.Clone()

	client := &Client{
		URL:             url,
		Model:           model,
		Effort:          effort,
		MaxOutputTokens: maxOutputTokens,
		tokens:          noTokens{},
		session:         newToken(),
		requests:        req.NewStreaming(responseHeaderTimeout, streamIdleTimeout),
		headerSource: func(Token) http.Header {
			header := fixedHeader.Clone()
			if header == nil {
				header = http.Header{}
			}
			header.Set("Accept", "text/event-stream")

			return header
		},
	}

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

type noTokens struct{}

func (self noTokens) Token() (Token, error) {
	return Token{}, nil
}

func (self *Client) SetRequestHeader(name string, value string) {
	if self.extraHeader == nil {
		self.extraHeader = http.Header{}
	}

	self.extraHeader.Set(name, value)
}

func (self *Client) UseSession(id string) {
	self.session = sessionToken(id)
}

func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
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

func (self *Client) AddUserMessage(text string) {
	self.history = append(self.history, encodeItem(userMessage{Role: "user", Content: text}))
}

func (self *Client) AddToolResults(results []agent.ToolCallResult) {
	for _, result := range results {
		self.history = append(self.history, encodeItem(toolOutput{
			Type:   "function_call_output",
			CallID: result.ID,
			Output: encodeToolOutput(result),
		}))
	}
}

func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

func (self *Client) Load(items []json.RawMessage) {
	self.history = slices.Clone(items)
	self.requestHistory.Reset()
}

func encodeItem(item any) json.RawMessage {
	encodedItem, _ := json.Marshal(item) //nolint:errchkjson // the wire items are plain structs

	return encodedItem
}

func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	reply, err := self.post(ctx, yield)
	if err != nil {
		self.history = append(self.history, reply.prose(isFinalFailure(err))...)

		return agent.Reply{}, err
	}

	self.history = append(self.history, reply.items...)

	return agent.Reply{Calls: reply.calls(), Usage: reply.usage}, nil
}

func (self *Client) observedRequests() *req.Client {
	client := req.New(asideTimeout)
	if self.observer != nil {
		client.Observe(self.observer)
	}

	return client
}

func (self *Client) post(ctx context.Context, yield agent.Yield) (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	stream, responseHeader, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers(token))
	isUsageLimited := usageLimitReached(err)
	windows := self.recordUsageWindows(responseHeader, time.Now(), isUsageLimited)
	if err != nil {
		if isUsageLimited {
			return reply{}, &agent.UsageLimitError{Cause: err, Windows: windows}
		}

		return reply{}, err
	}
	defer func() { _ = stream.Close() }()

	return readReply(stream, yield)
}

func usageLimitReached(err error) bool {
	refusal, isRefusal := errors.AsType[*req.StatusError](err)

	return isRefusal && refusal.Code == "usage_limit_reached"
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
			return fmt.Errorf("codex: %s is empty", setting.name)
		}
	}

	if self.Effort == "" && self.isEffortRequired {
		return errors.New("codex: Effort is empty")
	}

	return nil
}

func (self *Client) serviceTier() string {
	if self.IsFast {
		return fastServiceTier
	}

	return ""
}

func (self *Client) requestBody() request {
	return request{
		Model:             self.Model,
		ServiceTier:       self.serviceTier(),
		MaxOutputTokens:   self.MaxOutputTokens,
		Store:             false,
		Stream:            true,
		Input:             self.requestHistory.Prepare(self.history),
		Reasoning:         reasoning{Effort: self.Effort, Summary: Summary},
		Include:           []string{"reasoning.encrypted_content"},
		PromptCacheKey:    self.session,
		ToolChoice:        "auto",
		ParallelToolCalls: true,
		Tools:             self.tools,
		Instructions:      self.instructions,
	}
}

func (self *Client) headers(token Token) http.Header {
	header := self.headerSource(token)
	for name, values := range self.extraHeader {
		header[name] = slices.Clone(values)
	}

	return header
}

func (self *Client) codexHeaders(token Token) http.Header {
	return requestHeaders(token, self.session, routingHint(self.Model, self.serviceTier()))
}

type request struct {
	Model             string         `json:"model"`
	ServiceTier       string         `json:"service_tier,omitempty"`
	MaxOutputTokens   int            `json:"max_output_tokens,omitempty"`
	Store             bool           `json:"store"`
	Tools             []functionTool `json:"tools"`
	Instructions      string         `json:"instructions,omitempty"`
	ParallelToolCalls bool           `json:"parallel_tool_calls"`

	Stream bool `json:"stream"`

	Input []json.RawMessage `json:"input"`

	Include []string `json:"include"`

	PromptCacheKey string `json:"prompt_cache_key"`

	ToolChoice string `json:"tool_choice"`

	Reasoning reasoning `json:"reasoning"`
}

type reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type toolOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}

type inputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

const (
	imageDetail       = "high"
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

	content := []inputContent{
		{Type: "input_text", Text: text},
		{Type: "input_text", Text: attachmentNotice},
	}
	content = append(content, inputContent{
		Type: "input_image",
		ImageURL: fmt.Sprintf(
			"data:%s;base64,%s",
			result.Image.MediaType,
			base64.StdEncoding.EncodeToString(result.Image.Data),
		),
		Detail: imageDetail,
	})

	return content
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}

func sessionToken(id string) string {
	sum := sha256.Sum256([]byte(id))

	return hex.EncodeToString(sum[:])
}
