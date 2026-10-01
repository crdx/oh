package opencodego

import "strings"

type wireFormat int

const (
	completionsWire wireFormat = iota
	responsesWire
	messagesWire
)

func wireFor(id string) wireFormat {
	switch {
	case strings.HasPrefix(id, "grok-"),
		strings.HasSuffix(id, "-contributor"),
		strings.HasSuffix(id, "-luna"):
		return responsesWire
	case strings.HasPrefix(id, "minimax-"),
		strings.HasPrefix(id, "qwen"):
		return messagesWire
	default:
		return completionsWire
	}
}
