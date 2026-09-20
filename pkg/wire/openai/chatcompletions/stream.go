package chatcompletions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"crdx.org/oh/internal/sse"
	"crdx.org/oh/pkg/agent"
)

const donePayload = "[DONE]"

var ErrTruncated = sse.ErrTruncated

var ErrIncomplete = errors.New("the response was cut short")

type reply struct {
	content         string
	reasoning       string
	tools           map[int]*toolCall
	usage           agent.Usage
	stopReason      string
	isReasoningOpen bool
	isContentOpen   bool
}

func (self *reply) isEmpty() bool {
	return self.content == "" && self.reasoning == "" && len(self.tools) == 0
}

func (self *reply) message() message {
	return message{
		Role:             "assistant",
		Content:          self.content,
		ReasoningContent: self.reasoning,
		ToolCalls:        self.orderedToolCalls(),
	}
}

func (self *reply) hasSpoken() bool {
	return self.content != "" || self.reasoning != ""
}

func (self *reply) prose() message {
	proseMessage := self.message()
	proseMessage.ToolCalls = nil

	return proseMessage
}

func (self *reply) calls() []agent.ToolCall {
	toolCalls := self.orderedToolCalls()
	calls := make([]agent.ToolCall, len(toolCalls))
	for i, call := range toolCalls {
		calls[i] = agent.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		}
	}
	return calls
}

func (self *reply) orderedToolCalls() []toolCall {
	indexes := make([]int, 0, len(self.tools))
	for i := range self.tools {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)

	calls := make([]toolCall, 0, len(indexes))
	for _, index := range indexes {
		calls = append(calls, *self.tools[index])
	}
	return calls
}

type chunk struct {
	Choices []choice    `json:"choices"`
	Error   *chunkError `json:"error"`
	Usage   *usage      `json:"usage"`
}

type usage struct {
	PromptTokens     int                 `json:"prompt_tokens"`
	CompletionTokens int                 `json:"completion_tokens"`
	Details          *promptCacheDetails `json:"prompt_tokens_details"`
}

type promptCacheDetails struct {
	ReadTokens  int `json:"cached_tokens"`
	WriteTokens int `json:"cache_write_tokens"`
}

func (self usage) normalised() agent.Usage {
	normalisedUsage := agent.Usage{InputTokens: self.PromptTokens, OutputTokens: self.CompletionTokens}
	if self.Details != nil {
		normalisedUsage.Cache = &agent.CacheUsage{
			ReadTokens:  self.Details.ReadTokens,
			WriteTokens: self.Details.WriteTokens,
		}
	}
	return normalisedUsage
}

type choice struct {
	Delta        delta  `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type delta struct {
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	Reasoning        string          `json:"reasoning"`
	ToolCalls        []toolCallDelta `json:"tool_calls"`

	Refusal string `json:"refusal"`
}

type toolCallDelta struct {
	Index    int           `json:"index"`
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function functionDelta `json:"function"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chunkError struct {
	Message string `json:"message"`
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	reply := reply{tools: make(map[int]*toolCall)}
	err := sse.Read(body, func(payload string) (bool, error) {
		return reply.step(payload, yield)
	})
	return reply, err
}

var cutShort = []string{"length", "content_filter"}

func parseChunk(payload string) (chunk, error) {
	var zeroChunk chunk
	var chunk chunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return zeroChunk, fmt.Errorf("the endpoint sent something unreadable: %s", payload)
	}

	return chunk, nil
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	if payload == donePayload {
		if slices.Contains(cutShort, self.stopReason) {
			return true, ErrIncomplete
		}

		if self.completeReasoning(yield) || self.completeContent(yield) {
			return true, nil
		}

		return true, nil
	}

	chunk, err := parseChunk(payload)
	if err != nil {
		return true, err
	}
	if chunk.Error != nil {
		return true, errors.New(chunk.Error.Message)
	}
	if chunk.Usage != nil && chunk.Usage.PromptTokens > 0 {
		self.usage = chunk.Usage.normalised()
	}
	if len(chunk.Choices) == 0 {
		return false, nil
	}

	if reason := chunk.Choices[0].FinishReason; reason != "" {
		self.stopReason = reason
	}

	delta := chunk.Choices[0].Delta
	reasoning := delta.ReasoningContent
	if reasoning == "" {
		reasoning = delta.Reasoning
	}
	if reasoning != "" {
		self.reasoning += reasoning
		self.isReasoningOpen = true
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: reasoning}) {
			return true, nil
		}
	}
	sentence := delta.Content
	if sentence == "" {
		sentence = delta.Refusal
	}
	if sentence != "" {
		if self.completeReasoning(yield) {
			return true, nil
		}

		self.content += sentence
		self.isContentOpen = true
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: sentence}) {
			return true, nil
		}
	}

	if len(delta.ToolCalls) > 0 && self.completeReasoning(yield) {
		return true, nil
	}

	for _, fragment := range delta.ToolCalls {
		call := self.tools[fragment.Index]
		if call == nil {
			call = &toolCall{}
			self.tools[fragment.Index] = call
		}
		if fragment.ID != "" {
			call.ID = fragment.ID
		}
		if fragment.Type != "" {
			call.Type = fragment.Type
		}
		call.Function.Name += fragment.Function.Name
		call.Function.Arguments += fragment.Function.Arguments
	}

	return false, nil
}

func (self *reply) completeReasoning(yield agent.Yield) bool {
	if !self.isReasoningOpen {
		return false
	}

	self.isReasoningOpen = false
	return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true, Usage: self.reportedUsage()})
}

func (self *reply) completeContent(yield agent.Yield) bool {
	if !self.isContentOpen {
		return false
	}

	self.isContentOpen = false
	return !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true, Usage: self.reportedUsage()})
}

func (self *reply) reportedUsage() *agent.Usage {
	if self.usage.InputTokens <= 0 {
		return nil
	}

	usage := self.usage
	return &usage
}
