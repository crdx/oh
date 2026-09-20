package backend

import (
	"crdx.org/oh/pkg/provider/opencodego"

	"crdx.org/oh/internal/app/model"
)

func connectOpencodeGo(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	token := standInToken

	if endpoint == "" {
		endpoint = opencodego.EndpointURL

		var err error
		if token, err = opencodego.StoredKey(); err != nil {
			return nil, err
		}
	}

	client, err := opencodego.New(endpoint, token, choice.ID, effort, choice.MaxOutputTokens)
	if err != nil {
		return nil, err
	}

	if endpoint == opencodego.EndpointURL {
		client.UsageURL = opencodego.UsageEndpointURL
	}

	return &Connection{Client: client, ToolsSize: opencodego.ToolsSize}, nil
}
