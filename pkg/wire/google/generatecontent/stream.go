package generatecontent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"crdx.org/oh/internal/sse"
	"crdx.org/oh/pkg/agent"
)

var ErrTruncated = sse.ErrTruncated

var ErrIncomplete = errors.New("the response was cut short")

const (
	finishedReason      = "STOP"
	outOfRoomReason     = "MAX_TOKENS"
	malformedCallReason = "MALFORMED_FUNCTION_CALL"
)

type malformedCallError struct {
	explanation string
}

func (self malformedCallError) Error() string {
	if self.explanation == "" {
		return "the model made a malformed function call"
	}

	return "the model made a malformed function call: " + self.explanation
}

func (malformedCallError) Retriable() bool {
	return true
}

func (malformedCallError) RetryAfter() time.Duration {
	return 0
}

func (malformedCallError) Resumable() bool {
	return true
}

func (self malformedCallError) correction() string {
	if self.explanation == "" {
		return "Your previous response could not be used because its function call was malformed. Try again with a well-formed function call."
	}

	return fmt.Sprintf(
		"Your previous response could not be used because its function call was malformed (%s). Try again with a well-formed function call.",
		self.explanation,
	)
}

type stopError struct {
	reason      string
	explanation string
}

func (self stopError) Error() string {
	if self.explanation == "" {
		return "the endpoint stopped the response for " + self.reason
	}

	return fmt.Sprintf("the endpoint stopped the response for %s: %s", self.reason, self.explanation)
}

type promptRefusalError struct {
	reason      string
	explanation string
}

func (self promptRefusalError) Error() string {
	if self.explanation == "" {
		return "the endpoint refused the prompt for " + self.reason
	}

	return fmt.Sprintf("the endpoint refused the prompt for %s: %s", self.reason, self.explanation)
}

type chunk struct {
	Candidates     []candidate     `json:"candidates"`
	Usage          *usageMetadata  `json:"usageMetadata"`
	PromptFeedback *promptFeedback `json:"promptFeedback"`
	Error          *chunkError     `json:"error"`
}

type candidate struct {
	Content struct {
		Parts []json.RawMessage `json:"parts"`
	} `json:"content"`
	FinishReason  string `json:"finishReason"`
	FinishMessage string `json:"finishMessage"`
}

type promptFeedback struct {
	BlockReason        string `json:"blockReason"`
	BlockReasonMessage string `json:"blockReasonMessage"`
}

type chunkError struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

type usageMetadata struct {
	PromptTokens        int `json:"promptTokenCount"`
	CachedTokens        int `json:"cachedContentTokenCount"`
	CandidatesTokens    int `json:"candidatesTokenCount"`
	ThoughtsTokens      int `json:"thoughtsTokenCount"`
	ToolUsePromptTokens int `json:"toolUsePromptTokenCount"`
}

func (self usageMetadata) normalised() agent.Usage {
	return agent.Usage{
		InputTokens:  self.PromptTokens + self.ToolUsePromptTokens,
		OutputTokens: self.CandidatesTokens + self.ThoughtsTokens,
		Cache:        &agent.CacheUsage{ReadTokens: self.CachedTokens},
	}
}

type arrivingPart struct {
	Text             *string         `json:"text"`
	IsThought        bool            `json:"thought"`
	ThoughtSignature string          `json:"thoughtSignature"`
	FunctionCall     json.RawMessage `json:"functionCall"`
}

type arrivingText struct {
	text      strings.Builder
	isThought bool
}

func (self *arrivingText) part() textPart {
	return textPart{Text: self.text.String(), IsThought: self.isThought}
}

type receivedPart struct {
	raw    json.RawMessage
	isText bool
}

type reply struct {
	parts           []receivedPart
	pendingText     *arrivingText
	usage           agent.Usage
	finishReason    string
	finishMessage   string
	refusal         *promptRefusalError
	isReasoningOpen bool
	isAnswerOpen    bool
	isAbandoned     bool
}

func readReply(body io.Reader, yield agent.Yield) (*reply, error) {
	reply := &reply{}
	err := sse.Read(body, func(payload string) (bool, error) {
		return reply.step(payload, yield)
	})
	reply.settle()

	switch {
	case reply.isAbandoned:
		return reply, nil
	case !errors.Is(err, sse.ErrTruncated):
		return reply, err
	case reply.refusal != nil:
		return reply, *reply.refusal
	case reply.finishReason == "":
		return reply, err
	}

	return reply, reply.finish(yield)
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	var streamChunk chunk
	if err := json.Unmarshal([]byte(payload), &streamChunk); err != nil {
		return true, fmt.Errorf("the endpoint sent something unreadable: %s", payload)
	}

	if streamChunk.Error != nil {
		if streamChunk.Error.Message == "" {
			return true, errors.New(streamChunk.Error.Status)
		}

		return true, errors.New(streamChunk.Error.Message)
	}

	if streamChunk.Usage != nil && streamChunk.Usage.PromptTokens > 0 {
		self.usage = streamChunk.Usage.normalised()
	}

	if feedback := streamChunk.PromptFeedback; feedback != nil && feedback.BlockReason != "" {
		self.refusal = &promptRefusalError{reason: feedback.BlockReason, explanation: feedback.BlockReasonMessage}
	}

	if len(streamChunk.Candidates) == 0 {
		return false, nil
	}

	firstCandidate := streamChunk.Candidates[0]
	for _, raw := range firstCandidate.Content.Parts {
		if !self.add(raw, yield) {
			self.isAbandoned = true
			return true, nil
		}
	}

	if firstCandidate.FinishReason != "" {
		self.finishReason = firstCandidate.FinishReason
		self.finishMessage = firstCandidate.FinishMessage
	}

	return false, nil
}

func (self *reply) add(raw json.RawMessage, yield agent.Yield) bool {
	var part arrivingPart
	if json.Unmarshal(raw, &part) != nil || part.Text == nil || len(part.FunctionCall) > 0 {
		self.settle()
		self.parts = append(self.parts, receivedPart{raw: raw})

		return len(part.FunctionCall) == 0 || !self.completeReasoning(yield)
	}

	if self.pendingText != nil && (self.pendingText.isThought != part.IsThought || part.ThoughtSignature != "") {
		self.settle()
	}

	if part.ThoughtSignature != "" {
		self.parts = append(self.parts, receivedPart{raw: raw, isText: true})
	} else {
		if self.pendingText == nil {
			self.pendingText = &arrivingText{isThought: part.IsThought}
		}
		self.pendingText.text.WriteString(*part.Text)
	}

	if *part.Text == "" {
		return true
	}

	if part.IsThought {
		if self.completeAnswer(yield) {
			return false
		}

		self.isReasoningOpen = true

		return yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: *part.Text})
	}

	if self.completeReasoning(yield) {
		return false
	}

	self.isAnswerOpen = true

	return yield(agent.Output{Kind: agent.ModelMessageEvent, Text: *part.Text})
}

func (self *reply) settle() {
	if self.pendingText == nil {
		return
	}

	if self.pendingText.text.Len() > 0 {
		self.parts = append(self.parts, receivedPart{raw: encode(self.pendingText.part()), isText: true})
	}
	self.pendingText = nil
}

func (self *reply) finish(yield agent.Yield) error {
	switch self.finishReason {
	case finishedReason:
	case outOfRoomReason:
		return ErrIncomplete
	case malformedCallReason:
		return malformedCallError{explanation: self.finishMessage}
	default:
		return stopError{reason: self.finishReason, explanation: self.finishMessage}
	}

	if self.completeReasoning(yield) || self.completeAnswer(yield) {
		self.isAbandoned = true
	}

	return nil
}

func (self *reply) completeReasoning(yield agent.Yield) bool {
	if !self.isReasoningOpen {
		return false
	}

	self.isReasoningOpen = false
	return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true, Usage: self.reportedUsage()})
}

func (self *reply) completeAnswer(yield agent.Yield) bool {
	if !self.isAnswerOpen {
		return false
	}

	self.isAnswerOpen = false
	return !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true, Usage: self.reportedUsage()})
}

func (self *reply) reportedUsage() *agent.Usage {
	if self.usage.InputTokens <= 0 {
		return nil
	}

	usage := self.usage
	return &usage
}

func (self *reply) content() json.RawMessage {
	if len(self.parts) == 0 {
		return nil
	}

	parts := make([]json.RawMessage, len(self.parts))
	for i, part := range self.parts {
		parts[i] = part.raw
	}

	return encode(content{Role: modelRole, Parts: parts})
}

func (self *reply) prose() json.RawMessage {
	var parts []json.RawMessage
	for _, part := range self.parts {
		if part.isText {
			parts = append(parts, part.raw)
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return encode(content{Role: modelRole, Parts: parts})
}
