package generatecontent

import (
	"encoding/json"
	"fmt"
)

func chunk(parts string) string {
	return `{"candidates":[{"content":{"role":"model","parts":` + parts + `},"index":0}]}`
}

func Answer(text string) string {
	return chunk(fmt.Sprintf(`[{"text":%q}]`, text))
}

func Thought(text string) string {
	return chunk(fmt.Sprintf(`[{"text":%q,"thought":true}]`, text))
}

const signature = "c2ltdWxhdGVk"

func Call(index int, id string, name string, arguments string) string {
	if !json.Valid([]byte(arguments)) {
		arguments = "{}"
	}

	signatureField := ""
	if index == 0 {
		signatureField = fmt.Sprintf(`,"thoughtSignature":%q`, signature)
	}

	return chunk(fmt.Sprintf(`[{"functionCall":{"id":%q,"name":%q,"args":%s}%s}]`, id, name, arguments, signatureField))
}

func Finish(reason string, promptTokens int, cachedTokens int) string {
	return fmt.Sprintf(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":%q,"index":0}],`+
			`"usageMetadata":{"promptTokenCount":%d,"cachedContentTokenCount":%d,`+
			`"candidatesTokenCount":30,"thoughtsTokenCount":12,"totalTokenCount":%d}}`,
		reason,
		promptTokens,
		cachedTokens,
		promptTokens+42,
	)
}

const (
	Stopped   = "STOP"
	OutOfRoom = "MAX_TOKENS"
)

func Error(message string) string {
	return fmt.Sprintf(`{"error":{"code":500,"message":%q,"status":"INTERNAL"}}`, message)
}
