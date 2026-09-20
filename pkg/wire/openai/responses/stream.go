package responses

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"crdx.org/io/internal/sse"
	"crdx.org/io/pkg/agent"
)

const donePayload = "[DONE]"

var ErrIncomplete = errors.New("the response was cut short")

var ErrTruncated = sse.ErrTruncated

type reply struct {
	items            []json.RawMessage
	usage            agent.Usage
	summary          strings.Builder
	message          strings.Builder
	isSummarised     bool
	isRawReasoning   bool
	isMessageStarted bool
}

func (self *reply) calls() []agent.ToolCall {
	var calls []agent.ToolCall

	for _, raw := range self.items {
		if item := decodeItem(raw); item.Type == "function_call" {
			calls = append(calls, agent.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}

	return calls
}

func isFinalFailure(err error) bool {
	var retriable agent.Retriable

	return !errors.As(err, &retriable) || !retriable.Retriable()
}

func (self *reply) prose(isFinal bool) []json.RawMessage {
	keptItems := make([]json.RawMessage, 0, len(self.items))

	for _, raw := range self.items {
		if decodeItem(raw).Type != "function_call" {
			keptItems = append(keptItems, raw)
		}
	}

	if answer := self.message.String(); isFinal && answer != "" {
		keptItems = append(keptItems, partialMessage(answer))
	}

	for len(keptItems) > 0 && decodeItem(keptItems[len(keptItems)-1]).Type == "reasoning" {
		keptItems = keptItems[:len(keptItems)-1]
	}

	return keptItems
}

func partialMessage(text string) json.RawMessage {
	return encodeItem(map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []map[string]string{
			{"type": "output_text", "text": text},
		},
	})
}

func decodeItem(raw json.RawMessage) outputItem {
	var item outputItem
	_ = json.Unmarshal(raw, &item)

	return item
}

type outputItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type event struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	Message  string          `json:"message"`
	Error    *eventError     `json:"error"`
	Item     json.RawMessage `json:"item"`
	Part     *eventPart      `json:"part"`
	Response *eventResponse  `json:"response"`
}

type eventPart struct {
	Text string `json:"text"`
}

type eventResponse struct {
	Error *eventError   `json:"error"`
	Usage responseUsage `json:"usage"`
}

type responseUsage struct {
	InputTokens  int                   `json:"input_tokens"`
	OutputTokens int                   `json:"output_tokens"`
	Details      *responseCacheDetails `json:"input_tokens_details"`
}

type responseCacheDetails struct {
	ReadTokens  int `json:"cached_tokens"`
	WriteTokens int `json:"cache_write_tokens"`
}

func (self responseUsage) normalised() agent.Usage {
	usage := agent.Usage{InputTokens: self.InputTokens, OutputTokens: self.OutputTokens}
	if self.Details != nil {
		usage.Cache = &agent.CacheUsage{
			ReadTokens:  self.Details.ReadTokens,
			WriteTokens: self.Details.WriteTokens,
		}
	}
	return usage
}

type eventError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

var retriableFailures = map[string]bool{
	"server_error":         true,
	"server_is_overloaded": true,
	"rate_limit_exceeded":  true,
}

type StreamError struct {
	Code    string
	Message string
}

func (self *StreamError) Error() string {
	return self.Message
}

func (self *StreamError) Retriable() bool {
	return retriableFailures[self.Code]
}

func (*StreamError) RetryAfter() time.Duration {
	return 0
}

func (self *event) failure(payload string) error {
	if self.Response != nil && self.Response.Error != nil && self.Response.Error.Message != "" {
		return &StreamError{Code: self.Response.Error.Code, Message: self.Response.Error.Message}
	}

	if self.Error != nil && self.Error.Message != "" {
		return &StreamError{Code: self.Error.Code, Message: self.Error.Message}
	}

	if self.Message != "" {
		return errors.New(self.Message)
	}

	return errors.New(prettyJSON(payload))
}

func prettyJSON(payload string) string {
	var formattedJSON bytes.Buffer
	if err := json.Indent(&formattedJSON, []byte(payload), "", "  "); err != nil {
		return payload
	}

	return formattedJSON.String()
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	var reply reply

	err := sse.Read(body, func(payload string) (bool, error) {
		return reply.step(payload, yield)
	})

	return reply, err
}

func parseEvent(payload string) (event, bool) {
	var zeroEvent event
	var event event
	if json.Unmarshal([]byte(payload), &event) == nil {
		return event, true
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return zeroEvent, false
	}

	event = zeroEvent
	event.Type = envelope.Type
	return event, true
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	if payload == donePayload {
		self.completeOpenOutput(yield)
		return true, nil
	}

	event, readable := parseEvent(payload)
	if !readable {
		return false, nil
	}

	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta":
		return self.addMessageText(event.Delta, yield), nil

	case "response.output_text.done":
		return self.completeMessage(yield), nil

	case "response.reasoning_summary_text.delta":
		if event.Delta != "" {
			self.isSummarised = true
			self.summary.WriteString(event.Delta)

			if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: event.Delta}) {
				return true, nil
			}
		}

	case "response.reasoning_summary_part.done":
		if event.Part != nil && event.Part.Text != "" {
			if self.completeSummarisedReasoning(event.Part.Text, yield) {
				return true, nil
			}
		}

	case "response.reasoning_text.delta":
		if event.Delta != "" && !self.isSummarised {
			self.isRawReasoning = true
			if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: event.Delta}) {
				return true, nil
			}
		}

	case "response.reasoning_text.done":
		return self.completeRawReasoning(yield), nil

	case "response.output_item.done":
		if len(event.Item) > 0 {
			self.items = append(self.items, event.Item)
			item := decodeItem(event.Item)
			switch item.Type {
			case "reasoning":
				if self.completeRawReasoning(yield) {
					return true, nil
				}
			case "message":
				self.message.Reset()
				if self.completeMessage(yield) {
					return true, nil
				}
			}
		}

	case "response.completed", "response.done":
		if event.Response != nil {
			self.usage = event.Response.Usage.normalised()
		}
		self.completeOpenOutput(yield)
		return true, nil

	case "response.incomplete":
		return true, ErrIncomplete

	case "response.failed", "error":
		return true, event.failure(payload)
	}

	return false, nil
}

func (self *reply) addMessageText(text string, yield agent.Yield) bool {
	if text == "" {
		return false
	}

	if self.completeRawReasoning(yield) {
		return true
	}

	self.isMessageStarted = true
	self.message.WriteString(text)

	return !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: text})
}

func (self *reply) completeSummarisedReasoning(part string, yield agent.Yield) bool {
	self.isSummarised = true

	streamedSummary := self.summary.String()
	if streamedSummary == "" {
		self.summary.WriteString(part)
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: part}) {
			return true
		}
	} else if suffix, found := strings.CutPrefix(part, streamedSummary); found && suffix != "" {
		self.summary.WriteString(suffix)
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: suffix}) {
			return true
		}
	}

	self.summary.Reset()

	return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true, AwaitUsage: true})
}

func (self *reply) completeRawReasoning(yield agent.Yield) bool {
	if !self.isRawReasoning {
		return false
	}

	self.isRawReasoning = false
	return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true, AwaitUsage: true})
}

func (self *reply) completeMessage(yield agent.Yield) bool {
	if !self.isMessageStarted {
		return false
	}

	self.isMessageStarted = false
	return !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true, AwaitUsage: true})
}

func (self *reply) completeOpenOutput(yield agent.Yield) {
	if self.completeRawReasoning(yield) {
		return
	}
	self.completeMessage(yield)
}
