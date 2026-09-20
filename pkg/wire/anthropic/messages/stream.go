package messages

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/sse"
	"crdx.org/oh/pkg/agent"
)

var ErrIncomplete = errors.New("the response was cut short")

var ErrTruncated = sse.ErrTruncated

const (
	emptyArguments = "{}"
	refusalReason  = "refusal"
)

var cutShort = []string{"max_tokens", "model_context_window_exceeded"}

type block struct {
	kind      string
	index     int
	id        string
	name      string
	data      string
	text      strings.Builder
	signature strings.Builder
	arguments strings.Builder
	isDone    bool
}

func (self *block) argumentsOrEmpty() string {
	if self.arguments.Len() == 0 {
		return emptyArguments
	}

	return self.arguments.String()
}

func (self *block) hasObjectArguments() bool {
	var object map[string]json.RawMessage

	return json.Unmarshal([]byte(self.argumentsOrEmpty()), &object) == nil && object != nil
}

type reply struct {
	blocks          []*block
	stopReason      string
	stopExplanation string
	usage           agent.Usage
}

type usage struct {
	InputTokens   int `json:"input_tokens"`
	OutputTokens  int `json:"output_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
}

func (self usage) normalised() agent.Usage {
	return agent.Usage{
		InputTokens:  self.InputTokens + self.CacheRead + self.CacheCreation,
		OutputTokens: self.OutputTokens,
		Cache: &agent.CacheUsage{
			ReadTokens:  self.CacheRead,
			WriteTokens: self.CacheCreation,
		},
	}
}

func (self *reply) find(index int) *block {
	for _, heldBlock := range self.blocks {
		if heldBlock.index == index {
			return heldBlock
		}
	}

	return nil
}

func (self *reply) message() json.RawMessage {
	return self.assemble(true)
}

func (self *reply) prose() json.RawMessage {
	return self.assemble(false)
}

func (self *reply) assemble(shouldIncludeCalls bool) json.RawMessage {
	blocks := make([]json.RawMessage, 0, len(self.blocks))

	for _, heldBlock := range self.blocks {
		switch heldBlock.kind {
		case "text":
			if heldBlock.text.Len() > 0 {
				blocks = append(blocks, encodeItem(textBlock{Type: "text", Text: heldBlock.text.String()}))
			}

		case "redacted_thinking":
			blocks = append(blocks, encodeItem(redactedBlock{Type: "redacted_thinking", Data: heldBlock.data}))

		case "thinking":
			if heldBlock.signature.Len() > 0 {
				blocks = append(blocks, encodeItem(thinkingBlock{
					Type:      "thinking",
					Thinking:  heldBlock.text.String(),
					Signature: heldBlock.signature.String(),
				}))
			}

		case "tool_use":
			if heldBlock.isDone && shouldIncludeCalls {
				blocks = append(blocks, encodeItem(toolUse{
					Type:  "tool_use",
					ID:    heldBlock.id,
					Name:  heldBlock.name,
					Input: json.RawMessage(heldBlock.argumentsOrEmpty()),
				}))
			}
		}
	}

	if len(blocks) == 0 {
		return nil
	}

	return encodeItem(message{Role: assistantRole, Content: blocks})
}

func (self *reply) validateToolInputs() error {
	for _, heldBlock := range self.blocks {
		if heldBlock.kind == "tool_use" && heldBlock.isDone && !heldBlock.hasObjectArguments() {
			return invalidToolInputError{
				toolID:    heldBlock.id,
				toolName:  heldBlock.name,
				arguments: heldBlock.argumentsOrEmpty(),
			}
		}
	}

	return nil
}

func (self *reply) calls(knownTools []string) []agent.ToolCall {
	var calls []agent.ToolCall

	for _, heldBlock := range self.blocks {
		if heldBlock.kind == "tool_use" && heldBlock.isDone {
			calls = append(calls, agent.ToolCall{
				ID:        heldBlock.id,
				Name:      fromClaudeCodeName(heldBlock.name, knownTools),
				Arguments: heldBlock.argumentsOrEmpty(),
			})
		}
	}

	return calls
}

type thinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type redactedBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type toolUse struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type event struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	ContentBlock *startedBlock   `json:"content_block"`
	Delta        *eventDelta     `json:"delta"`
	Error        *eventError     `json:"error"`
	Message      *startedMessage `json:"message"`
	Usage        *usage          `json:"usage"`
}

type startedMessage struct {
	Usage *usage `json:"usage"`
}

type startedBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"`
}

type eventDelta struct {
	Type        string       `json:"type"`
	Text        string       `json:"text"`
	Thinking    string       `json:"thinking"`
	Signature   string       `json:"signature"`
	PartialJSON string       `json:"partial_json"`
	StopReason  string       `json:"stop_reason"`
	StopDetails *stopDetails `json:"stop_details"`
}

type stopDetails struct {
	Explanation string `json:"explanation"`
}

type eventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

var retriableFailures = map[string]bool{
	"overloaded_error": true,
	"api_error":        true,
	"rate_limit_error": true,
	"timeout_error":    true,
}

type streamError struct {
	kind    string
	message string
}

func (self *streamError) Error() string {
	return self.message
}

func (self *streamError) Retriable() bool {
	return retriableFailures[self.kind]
}

func (*streamError) RetryAfter() time.Duration {
	return 0
}

func (self *event) failure(payload string) error {
	if self.Error != nil && self.Error.Message != "" {
		return &streamError{kind: self.Error.Type, message: self.Error.Message}
	}

	return errors.New(payload)
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	var reply reply

	err := sse.Read(body, func(payload string) (bool, error) {
		return reply.step(payload, yield)
	})

	return reply, err
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	var event event
	if json.Unmarshal([]byte(payload), &event) != nil {
		return true, fmt.Errorf("the endpoint sent something unreadable: %s", payload)
	}

	switch event.Type {
	case "message_start":
		if event.Message != nil {
			self.recordStartUsage(event.Message.Usage)
		}

	case "content_block_start":
		self.open(event)

	case "content_block_delta":
		return self.add(event, yield), nil

	case "content_block_stop":
		return self.close(event, yield), nil

	case "message_delta":
		if event.Delta != nil {
			if event.Delta.StopReason != "" {
				self.stopReason = event.Delta.StopReason
			}
			if event.Delta.StopDetails != nil {
				self.stopExplanation = event.Delta.StopDetails.Explanation
			}
		}
		self.recordUsage(event.Usage)

	case "message_stop":
		switch {
		case slices.Contains(cutShort, self.stopReason):
			return true, ErrIncomplete
		case self.stopReason == refusalReason && self.stopExplanation != "":
			return true, errors.New(self.stopExplanation)
		case self.stopReason == refusalReason:
			return true, errors.New("the model refused the request")
		default:
			return true, nil
		}

	case "error":
		return true, event.failure(payload)
	}

	return false, nil
}

func (self *reply) recordStartUsage(usage *usage) {
	if usage == nil {
		return
	}

	startUsage := *usage
	startUsage.OutputTokens = 0
	self.recordUsage(&startUsage)
}

func (self *reply) recordUsage(usage *usage) {
	if usage == nil {
		return
	}

	normalisedUsage := usage.normalised()
	if normalisedUsage.InputTokens > 0 {
		outputTokens := self.usage.OutputTokens
		self.usage = normalisedUsage
		self.usage.OutputTokens = max(normalisedUsage.OutputTokens, outputTokens)
	}
	if normalisedUsage.OutputTokens > self.usage.OutputTokens {
		self.usage.OutputTokens = normalisedUsage.OutputTokens
	}
}

func (self *reply) reportedUsage() *agent.Usage {
	if self.usage.InputTokens <= 0 {
		return nil
	}

	usage := self.usage
	return &usage
}

func (self *reply) open(event event) {
	if event.ContentBlock == nil {
		return
	}

	openedBlock := &block{
		kind:  event.ContentBlock.Type,
		index: event.Index,
		id:    event.ContentBlock.ID,
		name:  event.ContentBlock.Name,
		data:  event.ContentBlock.Data,
	}

	if openedBlock.kind == "redacted_thinking" {
		openedBlock.isDone = true
	}

	self.blocks = append(self.blocks, openedBlock)
}

func (self *reply) add(event event, yield agent.Yield) bool {
	heldBlock := self.find(event.Index)
	if heldBlock == nil || event.Delta == nil {
		return false
	}

	switch event.Delta.Type {
	case "text_delta":
		heldBlock.text.WriteString(event.Delta.Text)

		return !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: event.Delta.Text})

	case "thinking_delta":
		heldBlock.text.WriteString(event.Delta.Thinking)

		return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: event.Delta.Thinking})

	case "signature_delta":
		heldBlock.signature.WriteString(event.Delta.Signature)

	case "input_json_delta":
		heldBlock.arguments.WriteString(event.Delta.PartialJSON)
	}

	return false
}

func (self *reply) close(event event, yield agent.Yield) bool {
	heldBlock := self.find(event.Index)
	if heldBlock == nil {
		return false
	}

	heldBlock.isDone = true

	switch {
	case heldBlock.kind == "text" && heldBlock.text.Len() > 0:
		return !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true, AwaitUsage: true})
	case heldBlock.kind == "thinking" && heldBlock.text.Len() > 0 && heldBlock.signature.Len() > 0:
		return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true, Usage: self.reportedUsage()})
	default:
		return false
	}
}
