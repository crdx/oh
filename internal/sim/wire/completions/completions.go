package completions

import (
	"fmt"
)

const Done = "[DONE]"

func chunk(delta string) string {
	return `{"object":"chat.completion.chunk","choices":[{"index":0,"delta":` + delta + `}]}`
}

func Answer(text string) string {
	return chunk(fmt.Sprintf(`{"role":"assistant","content":%q}`, text))
}

func Thought(text string) string {
	return chunk(fmt.Sprintf(`{"role":"assistant","reasoning_content":%q}`, text))
}

func Call(index int, id string, name string, arguments string) string {
	return chunk(fmt.Sprintf(
		`{"tool_calls":[{"index":%d,"id":%q,"type":"function",`+
			`"function":{"name":%q,"arguments":%q}}]}`,
		index, id, name, arguments,
	))
}

func Usage(promptTokens int, cachedTokens int) string {
	return fmt.Sprintf(
		`{"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":%d,`+
			`"completion_tokens":42,`+
			`"prompt_tokens_details":{"cached_tokens":%d,"cache_write_tokens":0}}}`,
		promptTokens,
		cachedTokens,
	)
}

func Finish(reason string) string {
	return fmt.Sprintf(
		`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":%q}]}`,
		reason,
	)
}

const (
	Stopped    = "stop"
	OutOfRoom  = "length"
	AskedTools = "tool_calls"
)

func Error(message string) string {
	return fmt.Sprintf(`{"error":{"message":%q}}`, message)
}
