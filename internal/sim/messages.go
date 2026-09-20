package sim

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crdx.org/oh/internal/sim/wire/messages"
)

const (
	freshTokens  = 120
	cachedTokens = 4000
)

type messagesDialect struct{}

func (self messagesDialect) Name() string {
	return Messages
}

func (self messagesDialect) Path() string {
	return "/messages"
}

type messagesBody struct {
	Model     string `json:"model"`
	Stream    bool   `json:"stream"`
	MaxTokens int    `json:"max_tokens"`

	System []struct {
		Text string `json:"text"`
	} `json:"system"`

	Messages []struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	} `json:"messages"`

	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type messagesBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"`
	Input     any    `json:"input"`
}

func (self messagesDialect) Read(_ *http.Request, raw []byte) (Request, bool) {
	var sentBody messagesBody
	if json.Unmarshal(raw, &sentBody) != nil {
		return Request{}, false
	}

	askedRequest := Request{
		API:       self.Name(),
		Model:     sentBody.Model,
		Streaming: sentBody.Stream,
	}

	for _, block := range sentBody.System {
		if askedRequest.Instructions == "" || len(sentBody.System) > 1 {
			askedRequest.Instructions = block.Text
		}
	}

	for _, message := range sentBody.Messages {
		askedRequest.Input = append(askedRequest.Input, messagesEntries(message.Role, message.Content)...)
	}

	for _, offeredTool := range sentBody.Tools {
		askedRequest.Tools = append(askedRequest.Tools, offeredTool.Name)
	}

	return askedRequest, true
}

func messagesEntries(role string, content []json.RawMessage) []Entry {
	entries := make([]Entry, 0, len(content))

	for _, raw := range content {
		var block messagesBlock
		if json.Unmarshal(raw, &block) != nil {
			continue
		}

		entry := Entry{Role: role, Raw: raw}

		switch block.Type {
		case "tool_use":
			entry.Type = CallMade
			entry.CallID = block.ID
			entry.Name = block.Name
			entry.Arguments = encodeArguments(block.Input)

		case "tool_result":
			entry.Type = CallOutput
			entry.CallID = block.ToolUseID
			entry.Output = flatten(block.Content)
			entry.Content = entry.Output

		default:
			entry.Type = Message
			entry.Content = block.Text
		}

		entries = append(entries, entry)
	}

	return entries
}

func encodeArguments(input any) string {
	if input == nil {
		return ""
	}

	encodedInput, err := json.Marshal(input)
	if err != nil {
		return ""
	}

	return string(encodedInput)
}

func (self messagesDialect) Check(scenario *Scenario, askedRequest Request) string {
	switch {
	case !askedRequest.Streaming:
		return "only streaming responses are supported"
	case askedRequest.Instructions == "":
		return "the request carried no instructions"
	case askedRequest.Model != scenario.Model:
		return fmt.Sprintf("the model %q is not available", askedRequest.Model)
	}

	if hangingCall := unansweredCall(askedRequest.Input); hangingCall != "" {
		return "No tool output found for function call " + hangingCall + "."
	}

	return ""
}

func (self messagesDialect) Play(stream *Stream, scenario *Scenario, turn Turn) {
	stream.Send(messages.Start(scenario.Model, freshTokens, cachedTokens))

	var index int

	for _, thought := range turn.Think {
		stream.Send(messages.ThinkingStart(index))
		stream.Type(func(word string) string { return messages.Thought(index, word) }, thought)
		stream.Send(messages.Signature(index, "sealed:"+thought))
		stream.Send(messages.BlockStop(index))

		index++
	}

	if turn.Say != "" {
		stream.Send(messages.TextStart(index))
		stream.Type(func(word string) string { return messages.Answer(index, word) }, turn.Say)

		if turn.Truncate {
			return
		}

		stream.Send(messages.BlockStop(index))

		index++
	}

	if turn.Truncate {
		return
	}

	for _, call := range turn.Calls {
		stream.Send(messages.ToolStart(index, stream.ID("toolu"), call.Name))
		stream.Send(messages.Arguments(index, call.Arguments))
		stream.Send(messages.BlockStop(index))

		index++
	}

	switch {
	case turn.ErrorEvent != "":
		stream.Send(turn.ErrorEvent)

		return
	case turn.Fail != "":
		stream.Send(messages.Error(turn.Fail))

		return
	case turn.Incomplete:
		stream.Send(messages.Stop(messages.OutOfRoom, freshTokens, cachedTokens))
	case len(turn.Calls) > 0:
		stream.Send(messages.Stop(messages.ToolUse, freshTokens, cachedTokens))
	default:
		stream.Send(messages.Stop(messages.EndTurn, freshTokens, cachedTokens))
	}

	stream.Send(messages.MessageStop)
}

func (self messagesDialect) Exhausted(stream *Stream, message string) {
	stream.Send(messages.Start("sim", freshTokens, cachedTokens))
	stream.Send(messages.TextStart(0))
	stream.Send(messages.Answer(0, message))
	stream.Send(messages.BlockStop(0))
	stream.Send(messages.Stop(messages.EndTurn, freshTokens, cachedTokens))
	stream.Send(messages.MessageStop)
}
