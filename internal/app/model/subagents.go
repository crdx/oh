package model

import (
	"strings"
)

func CanDelegateTo(parent Choice, candidate Choice, isAvailableToAll bool) bool {
	if (parent.Provider == OllamaProvider) != (candidate.Provider == OllamaProvider) {
		return false
	}
	return isAvailableToAll || IsFreeSubagentModel(candidate) || parent.Provider == candidate.Provider
}

func IsFreeSubagentModel(candidate Choice) bool {
	return candidate.Provider != OllamaProvider && strings.HasSuffix(candidate.ID, freeModelSuffix)
}
