package responses

import (
	"encoding/json"

	"crdx.org/oh/pkg/tool"
)

type functionTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Strict      bool   `json:"strict"`

	Schema tool.Schema `json:"parameters"`
}

func describe(tools []tool.Definition) []functionTool {
	offeredTools := make([]functionTool, len(tools))

	for i, offer := range tools {
		offeredTools[i] = functionTool{
			Type:        "function",
			Name:        offer.Name,
			Description: offer.Description,
			Strict:      false,
			Schema:      offer.Schema,
		}
	}

	return offeredTools
}

func ToolsSize(tools []tool.Tool) int {
	definitions := make([]tool.Definition, len(tools))
	for i, offeredTool := range tools {
		definitions[i] = tool.Describe(offeredTool)
	}

	encodedTools, _ := json.Marshal(describe(definitions)) //nolint:errchkjson // all fields have safe encoders
	return len(encodedTools)
}
