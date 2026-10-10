package migrate

import (
	"encoding/json"
	"fmt"
	"strings"
)

const historicalOpencodeGoProvider = "opencode-go"

const (
	historicalCompletionsWire = "completions"
	historicalResponsesWire   = "responses"
	historicalMessagesWire    = "messages"
)

func wiresJoinModelChoices(line map[string]json.RawMessage) error {
	if string(line["kind"]) != `"head"` {
		return nil
	}

	rawMeta, hasMeta := line["meta"]
	if !hasMeta {
		return nil
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		return fmt.Errorf("the session metadata could not be read: %w", err)
	}

	rawChoice, hasChoice := meta["model_choice"]
	if !hasChoice {
		return nil
	}

	var choice map[string]json.RawMessage
	if err := json.Unmarshal(rawChoice, &choice); err != nil {
		return fmt.Errorf("the model choice could not be read: %w", err)
	}

	provider, _ := historicalString(choice["provider"])
	modelID, _ := historicalString(choice["id"])
	if _, hasWire := choice["wire"]; hasWire || provider != historicalOpencodeGoProvider || modelID == "" {
		return nil
	}

	setHistoricalString(choice, "wire", historicalWireFor(modelID))

	restatedChoice, err := json.Marshal(choice)
	if err != nil {
		return err
	}
	meta["model_choice"] = restatedChoice

	restatedMeta, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	line["meta"] = restatedMeta

	return nil
}

func historicalWireFor(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "grok-"),
		strings.HasSuffix(modelID, "-contributor"),
		strings.HasSuffix(modelID, "-luna"):
		return historicalResponsesWire
	case strings.HasPrefix(modelID, "minimax-"),
		strings.HasPrefix(modelID, "qwen"):
		return historicalMessagesWire
	default:
		return historicalCompletionsWire
	}
}
