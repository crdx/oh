package backend

import (
	"crdx.org/oh/pkg/provider/codex"

	"crdx.org/oh/internal/app/model"
)

const webSearchModel = "gpt-5.6-terra"

func connectCodex(choice model.Choice, selection model.Selection, endpoint string) (*Connection, error) {
	tokens, address := codexCredentials(endpoint)

	client, err := codex.New(tokens, choice.ID, selection.Effort)
	if err != nil {
		return nil, err
	}
	client.URL = address
	client.IsFast = selection.IsFast

	search, err := newSearchClient(tokens, address)
	if err != nil {
		return nil, err
	}

	return &Connection{Client: client, ToolsSize: codex.ToolsSize, Search: search}, nil
}

func codexCredentials(endpoint string) (codex.TokenSource, string) {
	if endpoint != "" {
		return codex.Static(standInToken, standInToken), endpoint
	}

	return codex.StoredCredentials(), codex.Endpoint
}

func newSearchClient(tokens codex.TokenSource, address string) (*codex.SearchClient, error) {
	search, err := codex.NewSearch(tokens, webSearchModel)
	if err != nil {
		return nil, err
	}
	search.URL = address

	return search, nil
}
