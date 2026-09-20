package sim

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crdx.org/oh/internal/sim/wire/completions"
)

type completionsDialect struct{}

func (self completionsDialect) Name() string {
	return Completions
}

func (self completionsDialect) Path() string {
	return "/chat/completions"
}

type completionsBody struct {
	Model         string `json:"model"`
	Stream        bool   `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	Messages []struct {
		Role       string `json:"role"`
		Content    any    `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`

	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func (self completionsDialect) Read(_ *http.Request, raw []byte) (Request, bool) {
	var sentBody completionsBody
	if json.Unmarshal(raw, &sentBody) != nil {
		return Request{}, false
	}

	askedRequest := Request{
		API:          self.Name(),
		Model:        sentBody.Model,
		Streaming:    sentBody.Stream,
		IncludeUsage: sentBody.StreamOptions.IncludeUsage,
	}

	for i, message := range sentBody.Messages {
		if message.Role == "system" && i == 0 {
			askedRequest.Instructions = flatten(message.Content)

			continue
		}

		item, _ := json.Marshal(message) //nolint:errchkjson // read from JSON a moment ago

		entry := Entry{
			Type:    Message,
			Role:    message.Role,
			Content: flatten(message.Content),
			CallID:  message.ToolCallID,
			Raw:     item,
		}

		if message.Role == "tool" {
			entry.Type = CallOutput
			entry.Output = entry.Content
		}

		askedRequest.Input = append(askedRequest.Input, entry)

		for _, call := range message.ToolCalls {
			askedRequest.Input = append(askedRequest.Input, Entry{
				Type:      CallMade,
				CallID:    call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
				Raw:       item,
			})
		}
	}

	for _, offeredTool := range sentBody.Tools {
		askedRequest.Tools = append(askedRequest.Tools, offeredTool.Function.Name)
	}

	return askedRequest, true
}

func (self completionsDialect) Check(scenario *Scenario, askedRequest Request) string {
	switch {
	case !askedRequest.Streaming:
		return "only streaming responses are supported"
	case !askedRequest.IncludeUsage:
		return "stream usage was not requested"
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

func (self completionsDialect) Play(stream *Stream, _ *Scenario, turn Turn) {
	for _, thought := range turn.Think {
		stream.Type(completions.Thought, thought)
	}

	if turn.Say != "" {
		stream.Type(completions.Answer, turn.Say)
	}

	if turn.Truncate {
		return
	}

	for i, call := range turn.Calls {
		stream.Send(completions.Call(i, stream.ID("call"), call.Name, call.Arguments))
	}

	switch {
	case turn.ErrorEvent != "":
		stream.Send(turn.ErrorEvent)
	case turn.Fail != "":
		stream.Send(completions.Error(turn.Fail))
	default:
		stream.Send(completions.Finish(finishReason(turn)))
		stream.Send(completions.Usage(freshTokens+cachedTokens, cachedTokens))
	}

	stream.Send(completions.Done)
}

func finishReason(turn Turn) string {
	switch {
	case turn.Incomplete:
		return completions.OutOfRoom
	case len(turn.Calls) > 0:
		return completions.AskedTools
	default:
		return completions.Stopped
	}
}

func (self completionsDialect) Exhausted(stream *Stream, message string) {
	stream.Send(completions.Answer(message))
	stream.Send(completions.Finish(completions.Stopped))
	stream.Send(completions.Usage(freshTokens+cachedTokens, cachedTokens))
	stream.Send(completions.Done)
}
