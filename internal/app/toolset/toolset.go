package toolset

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/io/pkg/tool"
)

func Offers(enabledToolNames []string, name string) bool {
	return len(enabledToolNames) == 0 || slices.Contains(enabledToolNames, name)
}

func Names(tools []tool.Tool) []string {
	names := make([]string, len(tools))
	for at, availableTool := range tools {
		names[at] = availableTool.Name()
	}

	return names
}

func Partition(availableTools []tool.Tool, names []string) ([]string, []string) {
	availableNames := indexByName(availableTools)

	var present, absent []string
	for _, name := range names {
		if _, isAvailable := availableNames[name]; isAvailable {
			present = append(present, name)
			continue
		}

		absent = append(absent, name)
	}

	return present, absent
}

func Reduce(availableTools []tool.Tool, enabledToolNames []string) ([]tool.Tool, error) {
	if len(enabledToolNames) == 0 {
		return availableTools, nil
	}

	availableNames := indexByName(availableTools)
	enabledNames := make(map[string]struct{}, len(enabledToolNames))
	var unavailable []string
	for _, name := range enabledToolNames {
		if _, isEnabled := enabledNames[name]; isEnabled {
			continue
		}

		enabledNames[name] = struct{}{}
		if _, isAvailable := availableNames[name]; !isAvailable {
			unavailable = append(unavailable, name)
		}
	}
	if len(unavailable) > 0 {
		return nil, fmt.Errorf("tools not available: %s", strings.Join(unavailable, ", "))
	}

	tools := make([]tool.Tool, 0, len(enabledNames))
	for _, availableTool := range availableTools {
		if _, isEnabled := enabledNames[availableTool.Name()]; isEnabled {
			tools = append(tools, availableTool)
		}
	}

	return tools, nil
}

func indexByName(tools []tool.Tool) map[string]tool.Tool {
	indexedTools := make(map[string]tool.Tool, len(tools))
	for _, availableTool := range tools {
		indexedTools[availableTool.Name()] = availableTool
	}

	return indexedTools
}
