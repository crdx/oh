package responses

import (
	"fmt"
)

const Done = "[DONE]"

func CompletedResponse(inputTokens int, cachedTokens int) string {
	return fmt.Sprintf(
		`{"type":"response.completed","response":{"usage":{"input_tokens":%d,"output_tokens":42,`+
			`"input_tokens_details":{"cached_tokens":%d,"cache_write_tokens":0}}}}`,
		inputTokens,
		cachedTokens,
	)
}

const IncompleteResponse = `{"type":"response.incomplete"}`

func FailedResponse(message string) string {
	return fmt.Sprintf(`{"type":"response.failed","response":{"error":{"message":%q}}}`, message)
}

func Answer(text string) string {
	return fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, text)
}

func Thought(text string) string {
	return fmt.Sprintf(`{"type":"response.reasoning_summary_text.delta","delta":%q}`, text)
}

func ThinkingPart(text string) string {
	return fmt.Sprintf(
		`{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":%q}}`,
		text,
	)
}

func Item(item string) string {
	return `{"type":"response.output_item.done","item":` + item + `}`
}

func Call(id string, name string, arguments string) string {
	return Item(fmt.Sprintf(
		`{"type":"function_call","call_id":%q,"name":%q,"arguments":%q}`, id, name, arguments,
	))
}

func Message(text string) string {
	return Item(fmt.Sprintf(
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}`, text,
	))
}

func ReasoningItem(id string, summary string) string {
	return Item(fmt.Sprintf(
		`{"type":"reasoning","id":%q,"summary":[{"type":"summary_text","text":%q}],`+
			`"encrypted_content":%q}`,
		id, summary, "sealed:"+summary,
	))
}
