package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"crdx.org/oh/pkg/tool"
)

type Provider interface {
	Configure(systemPrompt string, tools []tool.Definition)
	AddUserMessage(text string)
	AddToolResults(toolResults []ToolCallResult)
	Send(ctx context.Context, yield Yield) (Reply, error)
}

var (
	ErrNoState       = errors.New("the provider does not expose conversation state")
	ErrStateReplaced = errors.New("the provider replaced append-only conversation state")
	ErrNoListing     = errors.New("it lists no models of its own, and only the registry describes it")
)

type State interface {
	Dump() []json.RawMessage
	Load(items []json.RawMessage)
}

type Model struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name,omitempty"`
	EffortLevels        []string     `json:"efforts,omitempty"`
	ContextWindowTokens int          `json:"context,omitempty"`
	MaxOutputTokens     int          `json:"output,omitempty"`
	Prices              *TokenPrices `json:"prices,omitempty"`
}

type TokenPrices struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

func (self TokenPrices) IsKnown() bool {
	return self.Input > 0 || self.Output > 0 || self.CacheRead > 0 || self.CacheWrite > 0
}

type PriceTier int

const (
	PriceUnknown PriceTier = iota
	PriceLow
	PriceMedium
	PriceHigh
	PriceExtreme
)

const (
	inputTokensPerOutputToken = 3
	mediumPriceFrom           = 2.0
	highPriceFrom             = 10.0
	extremePriceFrom          = 50.0
)

func (self TokenPrices) BlendedRate() float64 {
	return (self.Input*inputTokensPerOutputToken + self.Output) / (inputTokensPerOutputToken + 1)
}

func (self TokenPrices) Tier() PriceTier {
	switch rate := self.BlendedRate(); {
	case !self.IsKnown():
		return PriceUnknown
	case rate >= extremePriceFrom:
		return PriceExtreme
	case rate >= highPriceFrom:
		return PriceHigh
	case rate >= mediumPriceFrom:
		return PriceMedium
	default:
		return PriceLow
	}
}

func PriceTiers() []PriceTier {
	return []PriceTier{PriceLow, PriceMedium, PriceHigh, PriceExtreme}
}

const square = "◼"

var priceTierNames = map[PriceTier]string{
	PriceLow:     square,
	PriceMedium:  square + square,
	PriceHigh:    square + square + square,
	PriceExtreme: square + square + square + square,
}

func (self PriceTier) String() string {
	return priceTierNames[self]
}

type Lister interface {
	Models(context context.Context) ([]Model, error)
}

type UsageWindow struct {
	Duration  time.Duration `json:"duration"`
	Percent   float64       `json:"percent"`
	ResetsAt  time.Time     `json:"resets_at"`
	Scope     string        `json:"scope,omitempty"`
	IsLimited bool          `json:"limited,omitempty"`
}

type UsageReporter interface {
	IsAvailable() bool
	UsageWindows(context context.Context) ([]UsageWindow, error)
}

type UsageAvailability int

const (
	UsageAvailabilityUnknown UsageAvailability = iota
	UsageAvailabilityAllowed
	UsageAvailabilityLimited
)

type UsageProbe struct {
	Windows      []UsageWindow
	Availability UsageAvailability
	RefreshAfter time.Duration
}

type CacheLifetimeReporter interface {
	CacheLifetime() time.Duration
}

type UsageProber interface {
	ProbeUsage(context context.Context) (UsageProbe, error)
}

type UsageLimitError struct {
	Cause   error
	Windows []UsageWindow
}

func (self *UsageLimitError) Error() string {
	return self.Cause.Error()
}

func (self *UsageLimitError) Unwrap() error {
	return self.Cause
}

func (*UsageLimitError) Retriable() bool {
	return false
}

func (*UsageLimitError) RetryAfter() time.Duration {
	return 0
}

type Output struct {
	Kind       Kind
	Text       string
	Done       bool
	AwaitUsage bool
	Usage      *Usage
}

type Yield func(Output) bool

type Delta struct {
	Kind Kind
	Text string
}

type Update struct {
	Delta *Delta
	Event *Event
}

type Reply struct {
	Calls         []ToolCall
	Usage         Usage
	PrefixRewrite string
}

type Usage struct {
	InputTokens  int         `json:"input_tokens,omitempty"`
	OutputTokens int         `json:"output_tokens,omitempty"`
	Cache        *CacheUsage `json:"cache,omitempty"`
}

type CacheUsage struct {
	ReadTokens  int `json:"read_tokens"`
	WriteTokens int `json:"write_tokens,omitempty"`
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type ToolCallResult struct {
	ID      string
	Output  string
	Image   tool.Image
	IsError bool
}

type Kind string

const (
	StartupEvent         Kind = "session_startup"
	UserMessageEvent     Kind = "user_message"
	SilentTurnEvent      Kind = "silent_turn"
	PrefixRewriteEvent   Kind = "prefix_rewrite"
	CacheRebuildEvent    Kind = "cache_rebuild"
	ModelReasoningEvent  Kind = "model_reasoning"
	ModelMessageEvent    Kind = "model_message"
	ToolCallRequestEvent Kind = "tool_call_request"
	ToolCallResultEvent  Kind = "tool_call_result"
	StateChangeEvent     Kind = "state_change"
	InterruptionEvent    Kind = "turn_interruption"
	RetryingEvent        Kind = "request_retry"
	FailureEvent         Kind = "turn_failure"
)

const TitleStateKey = "title"

type TitleState struct {
	Title string `json:"title"`
}

func TitleFromEvent(event Event) (string, bool) {
	if event.Kind != StateChangeEvent || event.Name != TitleStateKey {
		return "", false
	}

	var state TitleState
	if err := json.Unmarshal(event.State, &state); err != nil || state.Title == "" {
		return "", false
	}

	return state.Title, true
}

type Retriable interface {
	error

	Retriable() bool
	RetryAfter() time.Duration
}

type Resumable interface {
	error

	Resumable() bool
}

type CallFaulted interface {
	error

	FaultedCall() ToolCall
}

type FallbackRendering struct {
	Subject      string               `json:"render,omitempty"`
	Note         string               `json:"detail,omitempty"`
	Emphasis     tool.Emphasis        `json:"emphasis,omitzero"`
	Continuation []tool.CallRendering `json:"continuation,omitempty"`
	ReadOnly     bool                 `json:"read_only,omitempty"`
}

func (self *FallbackRendering) Describe(toolCall tool.ToolCall) {
	self.Subject = toolCall.Subject()
	self.Note = toolCall.Qualifier()
	self.Emphasis = toolCall.Emphasis()
	self.Continuation = slices.Clone(toolCall.Continuation())
}

type Status string

const (
	InfoStatus      Status = "info"
	SuccessStatus   Status = "success"
	WarningStatus   Status = "warning"
	ErrorStatus     Status = "error"
	CancelledStatus Status = "cancelled"
)

type Event struct {
	FallbackRendering

	Kind      Kind                  `json:"kind"`
	Text      string                `json:"text,omitempty"`
	Failure   *Failure              `json:"failure,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Status    Status                `json:"status,omitempty"`
	Took      time.Duration         `json:"took,omitempty"`
	Attempt   int                   `json:"attempt,omitempty"`
	Metrics   *tool.ToolCallMetrics `json:"stats,omitempty"`
	Picture   *Picture              `json:"picture,omitempty"`
	State     json.RawMessage       `json:"state,omitempty"`
	Usage     *Usage                `json:"usage,omitempty"`
}

type Picture struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type Agent struct {
	provider         Provider
	registeredTools  map[string]tool.Tool
	enabledToolNames map[string]struct{}
	owners           map[string]tool.Tool
	state            []json.RawMessage
	cache            CacheReading
	cacheLifetime    time.Duration
	now              func() time.Time

	retryWaitsPassAtOnce bool
	storePicture         func(tool.Image) *Picture
}

func (self *Agent) StorePicturesWith(store func(tool.Image) *Picture) {
	self.storePicture = store
}
