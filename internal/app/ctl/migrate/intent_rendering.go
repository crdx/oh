package migrate

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

func intentsJoinRenderings(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		if string(event["kind"]) != `"tool_call_request"` {
			return nil
		}

		name, isNamed := historicalString(event["name"])
		if !isNamed {
			return nil
		}
		arguments := historicalArguments(event["arguments"])
		action, _ := historicalArgument(arguments, "action")

		switch {
		case name == "bash":
			storeHistoricalIntent(event, arguments)
		case name == "job" && action == "start":
			storeHistoricalIntent(event, arguments)
			if jobName, isPresent := historicalArgument(arguments, "name"); isPresent && jobName != "" {
				setHistoricalString(event, "introduces", jobName)
			}
		case name == "job" && slices.Contains(historicalMentioningActions, action):
			if names := historicalJobNames(arguments); len(names) > 0 {
				mentions, err := json.Marshal(names)
				if err != nil {
					return err
				}
				event["mentions"] = mentions
			}
		}

		return nil
	})
}

var historicalMentioningActions = []string{"wait", "status", "output", "stop", "discard"}

func storeHistoricalIntent(event map[string]json.RawMessage, arguments map[string]json.RawMessage) {
	if _, isStored := event["intent"]; isStored {
		return
	}

	intent, isPresent := historicalArgument(arguments, "intent")
	if intent = historicalSpokenIntent(intent); isPresent && intent != "" {
		setHistoricalString(event, "intent", intent)
	}
}

func historicalSpokenIntent(intent string) string {
	runes := []rune(strings.Join(strings.Fields(intent), " "))
	if len(runes) == 0 {
		return ""
	}

	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
