package generatecontent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"

	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

const emptyArguments = "{}"

type call struct {
	id        string
	wireID    string
	name      string
	arguments string
}

type calledPart struct {
	FunctionCall *struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"args"`
	} `json:"functionCall"`
}

func callsIn(item json.RawMessage, index int) []call {
	var turn content
	if json.Unmarshal(item, &turn) != nil || turn.Role != modelRole {
		return nil
	}

	var calls []call

	for partIndex, raw := range turn.Parts {
		var part calledPart
		if json.Unmarshal(raw, &part) != nil || part.FunctionCall == nil {
			continue
		}

		id := part.FunctionCall.ID
		if id == "" {
			id = fmt.Sprintf("call_%d_%d", index, partIndex)
		}

		calls = append(calls, call{
			id:        id,
			wireID:    part.FunctionCall.ID,
			name:      part.FunctionCall.Name,
			arguments: argumentsOf(part.FunctionCall.Arguments),
		})
	}

	return calls
}

func argumentsOf(raw json.RawMessage) string {
	trimmedArguments := bytes.TrimSpace(raw)
	if len(trimmedArguments) == 0 || bytes.Equal(trimmedArguments, []byte("null")) {
		return emptyArguments
	}

	var compactArguments bytes.Buffer
	if json.Compact(&compactArguments, trimmedArguments) != nil {
		return string(trimmedArguments)
	}

	return compactArguments.String()
}

func toolCalls(calls []call) []agent.ToolCall {
	agentCalls := make([]agent.ToolCall, len(calls))
	for i, made := range calls {
		agentCalls[i] = agent.ToolCall{ID: made.id, Name: made.name, Arguments: made.arguments}
	}

	return agentCalls
}

func merged(history []json.RawMessage) []content {
	joinedContents := make([]content, 0, len(history))

	for _, item := range history {
		var next content
		if json.Unmarshal(item, &next) != nil || len(next.Parts) == 0 {
			continue
		}

		if last := len(joinedContents) - 1; last >= 0 && joinedContents[last].Role == next.Role {
			joinedContents[last].Parts = joinParts(joinedContents[last].Parts, next.Parts)
			continue
		}

		joinedContents = append(joinedContents, next)
	}

	return joinedContents
}

type answeredPart struct {
	Text             *string         `json:"text"`
	FunctionResponse json.RawMessage `json:"functionResponse"`
}

func joinParts(parts []json.RawMessage, laterParts []json.RawMessage) []json.RawMessage {
	joinedParts := slices.Clone(parts)

	for _, part := range laterParts {
		answer := lastFunctionResponse(joinedParts)
		text, isText := textOf(part)
		if answer < 0 || !isText {
			joinedParts = append(joinedParts, part)
			continue
		}

		joinedParts[answer] = withFollowingText(joinedParts[answer], text)
	}

	return joinedParts
}

func lastFunctionResponse(parts []json.RawMessage) int {
	for index, part := range slices.Backward(parts) {
		var probe answeredPart
		if json.Unmarshal(part, &probe) == nil && len(probe.FunctionResponse) > 0 {
			return index
		}
	}

	return -1
}

func textOf(part json.RawMessage) (string, bool) {
	var probe answeredPart
	if json.Unmarshal(part, &probe) != nil || probe.Text == nil || len(probe.FunctionResponse) > 0 {
		return "", false
	}

	return *probe.Text, true
}

const followingTextSeparator = "\n\n"

func withFollowingText(part json.RawMessage, text string) json.RawMessage {
	var fields map[string]map[string]any
	if json.Unmarshal(part, &fields) != nil || fields["functionResponse"] == nil {
		return part
	}

	response, _ := fields["functionResponse"]["response"].(map[string]any)
	if response == nil {
		response = map[string]any{}
	}

	key := "output"
	if _, hasOutput := response[key]; !hasOutput {
		if _, hasError := response["error"]; hasError {
			key = "error"
		}
	}

	if earlierText, isText := response[key].(string); isText && earlierText != "" {
		text = earlierText + followingTextSeparator + text
	}
	response[key] = text
	fields["functionResponse"]["response"] = response

	return encode(fields)
}

func endsWithModelTurn(history []json.RawMessage) bool {
	for _, item := range slices.Backward(history) {
		var last content
		if json.Unmarshal(item, &last) != nil || len(last.Parts) == 0 {
			continue
		}

		return last.Role == modelRole
	}

	return false
}

type functionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Schema      tool.Schema `json:"parametersJsonSchema"`
}

type toolGroup struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

func describe(tools []tool.Definition) []functionDeclaration {
	declarations := make([]functionDeclaration, len(tools))
	for i, definition := range tools {
		declarations[i] = functionDeclaration{
			Name:        definition.Name,
			Description: definition.Description,
			Schema:      definition.Schema,
		}
	}

	return declarations
}

func ToolsSize(tools []tool.Tool) int {
	definitions := make([]tool.Definition, len(tools))
	for i, offeredTool := range tools {
		definitions[i] = tool.Describe(offeredTool)
	}

	encodedTools, _ := json.Marshal([]toolGroup{{FunctionDeclarations: describe(definitions)}}) //nolint:errchkjson // all fields have safe encoders
	return len(encodedTools)
}

var carriedMediaTypes = []string{"image/png", "image/jpeg", "image/webp"}

type imageHistory struct {
	preparedItems []json.RawMessage
}

func (self *imageHistory) prepare(items []json.RawMessage) []json.RawMessage {
	if len(items) < len(self.preparedItems) {
		self.reset()
	}

	for _, item := range items[len(self.preparedItems):] {
		self.preparedItems = append(self.preparedItems, boundJSONImages(item))
	}

	return self.preparedItems
}

func (self *imageHistory) reset() {
	self.preparedItems = nil
}

func boundJSONImages(payload []byte) []byte {
	var value any
	if json.Unmarshal(payload, &value) != nil {
		return payload
	}

	boundedValue, wasBounded := boundValueImages(value)
	if !wasBounded {
		return payload
	}

	encodedPayload, err := json.Marshal(boundedValue)
	if err != nil {
		return payload
	}

	return encodedPayload
}

func boundValueImages(value any) (any, bool) {
	switch typedValue := value.(type) {
	case map[string]any:
		wasBounded := false
		for key, item := range typedValue {
			if key == "inlineData" {
				if data, isData := item.(map[string]any); isData && boundInlineData(data) {
					wasBounded = true
					continue
				}
			}
			boundedItem, wasItemBounded := boundValueImages(item)
			typedValue[key] = boundedItem
			wasBounded = wasBounded || wasItemBounded
		}
		return typedValue, wasBounded
	case []any:
		wasBounded := false
		for index, item := range typedValue {
			boundedItem, wasItemBounded := boundValueImages(item)
			typedValue[index] = boundedItem
			wasBounded = wasBounded || wasItemBounded
		}
		return typedValue, wasBounded
	}

	return value, false
}

func boundInlineData(data map[string]any) bool {
	mediaType, hasMediaType := data["mimeType"].(string)
	encodedData, hasData := data["data"].(string)
	if !hasMediaType || !hasData || !imageutil.IsSupported(mediaType) {
		return false
	}

	decodedData, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return false
	}

	subject := tool.Image{MediaType: mediaType, Data: decodedData}
	carriedImage := imageutil.Bound(subject)
	if !slices.Contains(carriedMediaTypes, carriedImage.MediaType) {
		carriedImage = imageutil.AsPNG(carriedImage)
	}
	if carriedImage.MediaType == subject.MediaType && bytes.Equal(carriedImage.Data, subject.Data) {
		return false
	}

	data["mimeType"] = carriedImage.MediaType
	data["data"] = base64.StdEncoding.EncodeToString(carriedImage.Data)

	return true
}
