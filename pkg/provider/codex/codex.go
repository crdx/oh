package codex

import (
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/openai/responses"
)

const (
	Endpoint      = responses.Endpoint
	Originator    = responses.Originator
	SearchEffort  = responses.SearchEffort
	ClientVersion = responses.ClientVersion
)

var (
	Efforts       = responses.Efforts
	ErrIncomplete = responses.ErrIncomplete
	ErrTruncated  = responses.ErrTruncated
)

type Client struct {
	*responses.Client
}

type SearchClient = responses.SearchClient

type StreamError = responses.StreamError

func New(tokens TokenSource, model string, effort string) (*Client, error) {
	conversation, err := responses.New(tokens, model, effort)
	if err != nil {
		return nil, err
	}

	return &Client{Client: conversation}, nil
}

func Auth(model string, effort string) (*Client, error) {
	return New(StoredCredentials(), model, effort)
}

func NewSearch(tokens TokenSource, model string) (*SearchClient, error) {
	return responses.NewSearch(tokens, model)
}

func SupportsResponses(id string) bool {
	return responses.SupportsResponses(id)
}

func ToolsSize(tools []tool.Tool) int {
	return responses.ToolsSize(tools)
}
