package startup

import (
	"fmt"
	"io"
	"strings"

	"crdx.org/io/internal/app/edit"
)

func ReadPipedPrompt(source io.Reader) (string, error) {
	pipedInput, err := io.ReadAll(source)
	if err != nil {
		return "", fmt.Errorf("could not read the piped prompt: %w", err)
	}

	return strings.TrimSpace(string(pipedInput)), nil
}

func JoinPrompt(prompt string, addition string) string {
	switch {
	case prompt == "":
		return addition
	case addition == "":
		return prompt
	default:
		return prompt + "\n\n" + addition
	}
}

func JoinPipedPrompt(prompt string, pipedPrompt string) string {
	if prompt == "" || pipedPrompt == "" {
		return JoinPrompt(prompt, pipedPrompt)
	}

	return JoinPrompt(prompt, edit.FencedCodeBlock(pipedPrompt))
}
