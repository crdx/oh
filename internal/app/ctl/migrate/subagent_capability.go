package migrate

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	historicalSubagentToolName = "subagent"
	historicalSubagentsFlag    = "s"
	historicalShellFlag        = "x"
)

func subagentsBecomeACapability(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	if len(lines) == 0 {
		return lines, nil
	}

	offersSubagents, err := headOffersSubagents(lines[0])
	if err != nil {
		return nil, err
	}

	for index, line := range lines {
		err := with(line, func(event map[string]json.RawMessage) error {
			switch string(event["kind"]) {
			case `"mode_change"`:
				if !offersSubagents {
					return nil
				}
				return grantSubagents(event)
			case `"subagent_access_stop"`:
				if _, hasState := event["state"]; !hasState {
					event["state"] = json.RawMessage(`"` + historicalShellFlag + `"`)
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
	}

	return lines, nil
}

func headOffersSubagents(head map[string]json.RawMessage) (bool, error) {
	if string(head["kind"]) != `"head"` {
		return false, nil
	}

	rawMeta, hasMeta := head["meta"]
	if !hasMeta {
		return false, nil
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		return false, fmt.Errorf("the session metadata could not be read: %w", err)
	}

	rawTools, hasTools := meta["tools"]
	if !hasTools {
		return false, nil
	}

	var toolNames []string
	if err := json.Unmarshal(rawTools, &toolNames); err != nil {
		return false, fmt.Errorf("the offered tools could not be read: %w", err)
	}

	return slices.Contains(toolNames, historicalSubagentToolName), nil
}

func grantSubagents(event map[string]json.RawMessage) error {
	rawState, hasState := event["state"]
	if !hasState {
		return nil
	}

	if flags, areFlags := flagsOf(rawState); areFlags {
		encodedFlags, err := json.Marshal(withSubagentsFlag(flags))
		if err != nil {
			return err
		}
		event["state"] = encodedFlags
		return nil
	}

	var record map[string]json.RawMessage
	if err := json.Unmarshal(rawState, &record); err != nil {
		return fmt.Errorf("the mode could not be read: %w", err)
	}

	flags, areFlags := flagsOf(record["flags"])
	if !areFlags {
		return fmt.Errorf("the mode holds no flags: %s", rawState)
	}

	encodedFlags, err := json.Marshal(withSubagentsFlag(flags))
	if err != nil {
		return err
	}
	record["flags"] = encodedFlags

	encodedRecord, err := json.Marshal(record)
	if err != nil {
		return err
	}
	event["state"] = encodedRecord

	return nil
}

func withSubagentsFlag(flags string) string {
	if strings.Contains(flags, historicalSubagentsFlag) {
		return flags
	}

	return flags + historicalSubagentsFlag
}
