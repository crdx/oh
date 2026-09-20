package anthropic

import (
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/wire/anthropic/messages"
)

const (
	Endpoint = messages.Endpoint
	Identity = messages.Identity
)

var (
	Efforts       = messages.Efforts
	ErrIncomplete = messages.ErrIncomplete
	ErrTruncated  = messages.ErrTruncated
)

type Client struct {
	*messages.Client
}

func New(tokens TokenSource, model string, effort string, maxOutputTokens int) (*Client, error) {
	conversation, err := messages.New(tokens, model, effort, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Client{Client: conversation}, nil
}

func Auth(model string, effort string, maxOutputTokens int) (*Client, error) {
	return New(StoredCredentials(), model, effort, maxOutputTokens)
}

func SupportsAdaptiveThinking(id string) bool {
	return messages.SupportsAdaptiveThinking(id)
}

func ToolsSize(tools []tool.Tool) int {
	return messages.ToolsSize(tools)
}
