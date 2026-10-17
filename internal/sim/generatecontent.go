package sim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"

	"crdx.org/oh/internal/sim/wire/generatecontent"
)

type generateContentDialect struct{}

func (self generateContentDialect) Name() string {
	return GenerateContent
}

func (self generateContentDialect) Path() string {
	return generateContentMethod
}

const generateContentMethod = ":streamGenerateContent"

type generateContentBody struct {
	SystemInstruction *struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"systemInstruction"`

	Contents []struct {
		Role  string                `json:"role"`
		Parts []generateContentPart `json:"parts"`
	} `json:"contents"`

	Tools []struct {
		FunctionDeclarations []struct {
			Name string `json:"name"`
		} `json:"functionDeclarations"`
	} `json:"tools"`
}

type generateContentPart struct {
	Text         *string `json:"text"`
	FunctionCall *struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"args"`
	} `json:"functionCall"`
	FunctionResponse *struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Response struct {
			Output string `json:"output"`
			Error  string `json:"error"`
		} `json:"response"`
	} `json:"functionResponse"`
}

func (self generateContentDialect) Read(request *http.Request, raw []byte) (Request, bool) {
	var sentBody generateContentBody
	if json.Unmarshal(raw, &sentBody) != nil {
		return Request{}, false
	}

	askedRequest := Request{
		API:          self.Name(),
		Model:        strings.TrimSuffix(path.Base(request.URL.Path), generateContentMethod),
		Streaming:    request.URL.Query().Get("alt") == "sse",
		IncludeUsage: true,
	}

	if sentBody.SystemInstruction != nil {
		for _, part := range sentBody.SystemInstruction.Parts {
			askedRequest.Instructions += part.Text
		}
	}

	for _, sentContent := range sentBody.Contents {
		role := sentContent.Role
		if role == "model" {
			role = "assistant"
		}

		for _, part := range sentContent.Parts {
			item, _ := json.Marshal(part) //nolint:errchkjson // read from JSON a moment ago

			switch {
			case part.FunctionCall != nil:
				askedRequest.Input = append(askedRequest.Input, Entry{
					Type:      CallMade,
					CallID:    part.FunctionCall.ID,
					Name:      part.FunctionCall.Name,
					Arguments: string(part.FunctionCall.Arguments),
					Raw:       item,
				})
			case part.FunctionResponse != nil:
				output := part.FunctionResponse.Response.Output + part.FunctionResponse.Response.Error
				askedRequest.Input = append(askedRequest.Input, Entry{
					Type:    CallOutput,
					Role:    role,
					CallID:  part.FunctionResponse.ID,
					Name:    part.FunctionResponse.Name,
					Content: output,
					Output:  output,
					Raw:     item,
				})
			case part.Text != nil:
				askedRequest.Input = append(askedRequest.Input, Entry{
					Type:    Message,
					Role:    role,
					Content: *part.Text,
					Raw:     item,
				})
			}
		}
	}

	for _, group := range sentBody.Tools {
		for _, declaration := range group.FunctionDeclarations {
			askedRequest.Tools = append(askedRequest.Tools, declaration.Name)
		}
	}

	return askedRequest, true
}

func (self generateContentDialect) Check(scenario *Scenario, askedRequest Request) string {
	switch {
	case !askedRequest.Streaming:
		return "only streaming responses are supported"
	case askedRequest.Instructions == "":
		return "the request carried no instructions"
	case askedRequest.Model != scenario.Model:
		return fmt.Sprintf("the model %q is not available", askedRequest.Model)
	}

	if hangingCall := unansweredCall(askedRequest.Input); hangingCall != "" {
		return "No function response found for function call " + hangingCall + "."
	}

	return ""
}

func (self generateContentDialect) Play(stream *Stream, _ *Scenario, turn Turn) {
	for _, thought := range turn.Think {
		stream.Type(generatecontent.Thought, thought)
	}

	if turn.Say != "" {
		stream.Type(generatecontent.Answer, turn.Say)
	}

	if turn.Truncate {
		return
	}

	for i, call := range turn.Calls {
		stream.Send(generatecontent.Call(i, stream.ID("call"), call.Name, call.Arguments))
	}

	switch {
	case turn.ErrorEvent != "":
		stream.Send(turn.ErrorEvent)
	case turn.Fail != "":
		stream.Send(generatecontent.Error(turn.Fail))
	case turn.Incomplete:
		stream.Send(generatecontent.Finish(generatecontent.OutOfRoom, freshTokens+cachedTokens, cachedTokens))
	default:
		stream.Send(generatecontent.Finish(generatecontent.Stopped, freshTokens+cachedTokens, cachedTokens))
	}
}

func (self generateContentDialect) Exhausted(stream *Stream, message string) {
	stream.Send(generatecontent.Answer(message))
	stream.Send(generatecontent.Finish(generatecontent.Stopped, freshTokens+cachedTokens, cachedTokens))
}
