package model

import (
	"regexp"
	"slices"
)

var subagentModelsByProvider = map[string][]string{
	CodexProvider: {
		"gpt-6-luna",
	},
	AnthropicProvider: {
		"claude-haiku-5-5",
	},
	OpencodeGoProvider: {
		"deepseek-v4.1-flash",
	},
}

var freeSubagentModelPattern = regexp.MustCompile(`-free$`)

func CanDelegateTo(parent Choice, candidate Choice) bool {
	if parent.Provider == OllamaProvider || candidate.Provider == OllamaProvider {
		return parent.Provider == OllamaProvider && candidate.Provider == OllamaProvider
	}
	return parent.Provider == candidate.Provider && slices.Contains(subagentModelsByProvider[parent.Provider], candidate.ID) ||
		IsFreeSubagentModel(candidate)
}

func IsFreeSubagentModel(candidate Choice) bool {
	return candidate.Provider != OllamaProvider && freeSubagentModelPattern.MatchString(candidate.ID)
}
