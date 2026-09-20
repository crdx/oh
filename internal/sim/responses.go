package sim

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crdx.org/oh/internal/sim/wire/responses"
)

type responsesDialect struct{}

func (self responsesDialect) Name() string {
	return Responses
}

func (self responsesDialect) Path() string {
	return "/codex/responses"
}

type responsesBody struct {
	Model          string            `json:"model"`
	Stream         bool              `json:"stream"`
	Store          bool              `json:"store"`
	Instructions   string            `json:"instructions"`
	PromptCacheKey string            `json:"prompt_cache_key"`
	Input          []json.RawMessage `json:"input"`
	Tools          []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

func (self responsesDialect) Read(request *http.Request, raw []byte) (Request, bool) {
	var sentBody responsesBody
	if json.Unmarshal(raw, &sentBody) != nil {
		return Request{}, false
	}

	key := sentBody.PromptCacheKey
	if key == "" {
		key = request.Header.Get("Session_id")
	}

	askedRequest := Request{
		API:          self.Name(),
		Session:      key,
		Model:        sentBody.Model,
		Instructions: sentBody.Instructions,
		Streaming:    sentBody.Stream,
		IsStored:     sentBody.Store,
		Input:        responsesEntries(sentBody.Input),
	}

	for _, offeredTool := range sentBody.Tools {
		askedRequest.Tools = append(askedRequest.Tools, offeredTool.Name)
	}

	return askedRequest, true
}

func responsesEntries(items []json.RawMessage) []Entry {
	read := make([]Entry, 0, len(items))

	for _, item := range items {
		var sentItem struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content any    `json:"content"`
			CallID  string `json:"call_id"`
			Name    string `json:"name"`
			Output  string `json:"output"`
		}

		_ = json.Unmarshal(item, &sentItem)

		entry := Entry{
			Type:    sentItem.Type,
			Role:    sentItem.Role,
			Content: flatten(sentItem.Content),
			CallID:  sentItem.CallID,
			Name:    sentItem.Name,
			Output:  sentItem.Output,
			Raw:     item,
		}

		if entry.Type == "" && entry.Role != "" {
			entry.Type = Message
		}

		read = append(read, entry)
	}

	return read
}

func (self responsesDialect) Check(scenario *Scenario, askedRequest Request) string {
	switch {
	case !askedRequest.Streaming:
		return "only streaming responses are supported"
	case askedRequest.IsStored:
		return "this endpoint does not store conversations"
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

func (self responsesDialect) Play(stream *Stream, _ *Scenario, turn Turn) {
	for _, thought := range turn.Think {
		stream.Type(responses.Thought, thought)
		stream.Send(responses.ThinkingPart(thought))
		stream.Send(responses.ReasoningItem(stream.ID("rs"), thought))
	}

	if turn.Say != "" {
		stream.Type(responses.Answer, turn.Say)

		if turn.Truncate {
			return
		}

		stream.Send(responses.Message(turn.Say))
	}

	if turn.Truncate {
		return
	}

	for _, call := range turn.Calls {
		stream.Send(responses.Call(stream.ID("call"), call.Name, call.Arguments))
	}

	switch {
	case turn.ErrorEvent != "":
		stream.Send(turn.ErrorEvent)
	case turn.Fail != "":
		stream.Send(responses.FailedResponse(turn.Fail))
	case turn.Incomplete:
		stream.Send(responses.IncompleteResponse)
	default:
		stream.Send(responses.CompletedResponse(freshTokens+cachedTokens, cachedTokens))
	}

	stream.Send(responses.Done)
}

func (self responsesDialect) Exhausted(stream *Stream, message string) {
	stream.Send(responses.Answer(message))
	stream.Send(responses.Message(message))
	stream.Send(responses.CompletedResponse(freshTokens+cachedTokens, cachedTokens))
	stream.Send(responses.Done)
}
