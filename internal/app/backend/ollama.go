package backend

import (
	"os"

	"crdx.org/oh/pkg/provider/ollama"

	"crdx.org/oh/internal/app/model"
)

func connectOllama(choice model.Choice, effort string, endpoints EndpointSettings) (*Connection, error) {
	client, err := ollama.New(ollamaEndpointURL(endpoints), choice.ID, effort, choice.MaxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Connection{Client: client, ToolsSize: ollama.ToolsSize}, nil
}

func ollamaEndpointURL(endpoints EndpointSettings) string {
	if endpoints.OverrideURL != "" {
		return endpoints.OverrideURL
	}
	if host := os.Getenv(ollama.HostVariable); host != "" {
		return host
	}
	if endpoints.OllamaHost != "" {
		return endpoints.OllamaHost
	}
	return ollama.EndpointURL
}
