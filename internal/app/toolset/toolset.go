package toolset

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/pkg/tool"
)

var HeadlessTools = []string{"read", "ls", "find", "grep", "bash", "write", "edit"}

func Headless(tools []tool.Tool) []tool.Tool {
	return slices.DeleteFunc(slices.Clone(tools), func(candidate tool.Tool) bool {
		return !slices.Contains(HeadlessTools, candidate.Name())
	})
}

func WithheldFromHeadless(tools []tool.Tool) []string {
	var names []string
	for _, candidate := range tools {
		if !slices.Contains(HeadlessTools, candidate.Name()) {
			names = append(names, candidate.Name())
		}
	}
	return names
}

func RefuseOutsideHeadless(names []string) error {
	var refusedNames []string
	for _, name := range names {
		if !slices.Contains(HeadlessTools, name) && !slices.Contains(refusedNames, name) {
			refusedNames = append(refusedNames, name)
		}
	}
	if len(refusedNames) == 0 {
		return nil
	}

	return fmt.Errorf(
		"a headless session offers only %s, so it cannot offer %s",
		strings.Join(HeadlessTools, ", "),
		strings.Join(refusedNames, ", "),
	)
}

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
	VersionChanges  map[string]VersionChange
	CompatibleNames []string
}

func Restore(availableTools []tool.Tool, snapshots []tool.Snapshot, withheldNames []string) Restoration {
	availableByName := indexByName(availableTools)
	result := Restoration{
		OfferedTools:    make([]tool.Tool, 0, len(snapshots)),
		Availability:    make(Availability, len(snapshots)),
		VersionChanges:  make(map[string]VersionChange),
		CompatibleNames: make([]string, 0, len(snapshots)),
	}

	for _, snapshot := range snapshots {
		currentTool, isInstalled := availableByName[snapshot.Definition.Name]
		status := ToolMissing
		offeredTool := tool.Unavailable(snapshot, missingToolReason(snapshot.Definition.Name))
		if slices.Contains(withheldNames, snapshot.Definition.Name) {
			status = ToolWithheld
			offeredTool = tool.Unavailable(snapshot, withheldToolReason(snapshot.Definition.Name))
		}
		if isInstalled {
			status = ToolChanged
			offeredTool = tool.Unavailable(
				snapshot,
				changedToolReason(snapshot.Definition.Name, snapshot.Revision, currentTool.Revision()),
			)
			if tool.IsCompatible(currentTool, snapshot) {
				status = ToolAvailable
				offeredTool = tool.WithSnapshot(currentTool, snapshot)
				result.CompatibleNames = append(result.CompatibleNames, currentTool.Name())
			} else {
				result.VersionChanges[currentTool.Name()] = VersionChange{
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
