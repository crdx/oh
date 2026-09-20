package opencodego

import "strings"

func SupportsCompletions(id string) bool {
	switch {
	case strings.HasPrefix(id, "grok-"),
		strings.HasPrefix(id, "minimax-"),
		strings.HasPrefix(id, "qwen"),
		strings.HasSuffix(id, "-contributor"),
		strings.HasSuffix(id, "-luna"):
		return false
	default:
		return true
	}
}
