package toolset

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/pkg/tool"
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

func Combine(availableTools []tool.Tool, addedTools []tool.Tool) ([]tool.Tool, error) {
	availableNames := indexByName(availableTools)

	for _, addedTool := range addedTools {
		if _, isTaken := availableNames[addedTool.Name()]; isTaken {
			return nil, fmt.Errorf("%s is already a tool, so a custom tool cannot take its name", addedTool.Name())
		}

		availableNames[addedTool.Name()] = addedTool
		availableTools = append(availableTools, addedTool)
	}

	return availableTools, nil
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

type Restoration struct {
	OfferedTools    []tool.Tool
	RegisteredTools []tool.Tool
	Availability    Availability
	Transitions     map[string]CompatibilityTransition
	CompatibleNames []string
}

func Restore(availableTools []tool.Tool, snapshots []tool.Snapshot) Restoration {
	availableByName := indexByName(availableTools)
	result := Restoration{
		OfferedTools:    make([]tool.Tool, 0, len(snapshots)),
		Availability:    make(Availability, len(snapshots)),
		Transitions:     make(map[string]CompatibilityTransition),
		CompatibleNames: make([]string, 0, len(snapshots)),
	}

	for _, snapshot := range snapshots {
		currentTool, isInstalled := availableByName[snapshot.Definition.Name]
		status := ToolMissing
		offeredTool := tool.Unavailable(snapshot, missingToolReason(snapshot.Definition.Name))
		if isInstalled {
			status = ToolChanged
			offeredTool = tool.Unavailable(snapshot, changedToolReason(snapshot.Definition.Name))
			if tool.IsCompatible(currentTool, snapshot) {
				status = ToolAvailable
				offeredTool = currentTool
				result.CompatibleNames = append(result.CompatibleNames, currentTool.Name())
			} else if currentTool.Revision() != snapshot.Revision {
				result.Transitions[currentTool.Name()] = CompatibilityTransition{
					From: snapshot.Revision,
					To:   currentTool.Revision(),
				}
			}
		}

		result.Availability[snapshot.Definition.Name] = status
		result.OfferedTools = append(result.OfferedTools, offeredTool)
	}

	result.RegisteredTools = slices.Clone(result.OfferedTools)
	return result
}

func indexByName(tools []tool.Tool) map[string]tool.Tool {
	indexedTools := make(map[string]tool.Tool, len(tools))
	for _, availableTool := range tools {
		indexedTools[availableTool.Name()] = availableTool
	}

	return indexedTools
}
