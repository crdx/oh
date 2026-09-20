package messages

import (
	"encoding/json"
	"strings"

	"crdx.org/io/pkg/tool"
)

type functionTool struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Cache       *cacheControl `json:"cache_control,omitempty"`

	Schema tool.Schema `json:"input_schema"`
}

type cacheControl struct {
	Type string `json:"type"`
}

func ephemeral() *cacheControl {
	return &cacheControl{Type: "ephemeral"}
}

var claudeCodeNames = map[string]string{
	"read":            "Read",
	"write":           "Write",
	"edit":            "Edit",
	"bash":            "Bash",
	"grep":            "Grep",
	"glob":            "Glob",
	"askuserquestion": "AskUserQuestion",
	"enterplanmode":   "EnterPlanMode",
	"exitplanmode":    "ExitPlanMode",
	"killshell":       "KillShell",
	"notebookedit":    "NotebookEdit",
	"skill":           "Skill",
	"task":            "Task",
	"taskoutput":      "TaskOutput",
	"todowrite":       "TodoWrite",
	"webfetch":        "WebFetch",
	"websearch":       "WebSearch",
}

func toClaudeCodeName(name string) string {
	if canonical, isFound := claudeCodeNames[strings.ToLower(name)]; isFound {
		return canonical
	}

	return name
}

func fromClaudeCodeName(name string, knownNames []string) string {
	for _, candidate := range knownNames {
		if strings.EqualFold(candidate, name) {
			return candidate
		}
	}

	return name
}

func describe(tools []tool.Definition) []functionTool {
	offeredTools := make([]functionTool, len(tools))

	for i, offer := range tools {
		offeredTools[i] = functionTool{
			Name:        toClaudeCodeName(offer.Name),
			Description: offer.Description,
			Schema:      offer.Schema,
		}
	}

	if len(offeredTools) > 0 {
		offeredTools[len(offeredTools)-1].Cache = ephemeral()
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
