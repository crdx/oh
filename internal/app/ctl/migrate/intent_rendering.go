package migrate

import (
	"encoding/json"
	"strings"
	"unicode"
)

func intentsJoinRenderings(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		if string(event["kind"]) != `"tool_call_request"` {
			return nil
		}
		if _, isStored := event["intent"]; isStored {
			return nil
		}

		name, isNamed := historicalString(event["name"])
		if !isNamed {
			return nil
		}
		arguments := historicalArguments(event["arguments"])

		switch name {
		case "bash":
		case "job":
			if action, _ := historicalArgument(arguments, "action"); action != "start" {
				return nil
			}
		default:
			return nil
		}

		intent, isPresent := historicalArgument(arguments, "intent")
		if intent = historicalSpokenIntent(intent); !isPresent || intent == "" {
			return nil
		}
		setHistoricalString(event, "intent", intent)

		return nil
	})
}

func historicalSpokenIntent(intent string) string {
	runes := []rune(strings.Join(strings.Fields(intent), " "))
	if len(runes) == 0 || !unicode.IsUpper(runes[0]) {
		return string(runes)
	}
	if len(runes) > 1 && unicode.IsUpper(runes[1]) {
		return string(runes)
	}

	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
