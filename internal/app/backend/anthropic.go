package backend

import (
	"crdx.org/io/pkg/provider/anthropic"

	"crdx.org/io/internal/app/model"
)

func connectAnthropic(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	tokens := anthropic.StoredCredentials()
	address := anthropic.Endpoint

	if endpoint != "" {
		tokens = anthropic.Static(standInToken)
		address = endpoint
	}

	client, err := anthropic.New(tokens, choice.ID, effort, choice.MaxOutputTokens)
	if err != nil {
		return nil, err
	}
	client.URL = address

	return &Connection{Client: client, ToolsSize: anthropic.ToolsSize}, nil
}
