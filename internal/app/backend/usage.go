package backend

import (
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/anthropic"
	"crdx.org/io/pkg/provider/codex"
	"crdx.org/io/pkg/provider/opencodego"

	"crdx.org/io/internal/app/model"
)

const (
	anthropicLabel  = "Anthropic"
	openAILabel     = "OpenAI"
	opencodeGoLabel = "OpenCode Go"
)

type UsageSource struct {
	Provider string
	Label    string
	Reporter agent.UsageReporter

	HasIdleSessionWindow bool
}

func UsageSources() []UsageSource {
	return []UsageSource{
		{
			Provider:             model.AnthropicProvider,
			Label:                anthropicLabel,
			Reporter:             anthropicUsage(),
			HasIdleSessionWindow: true,
		},
		{
			Provider: model.CodexProvider,
			Label:    openAILabel,
			Reporter: codexUsage(),
		},
		{
			Provider: model.OpencodeGoProvider,
			Label:    opencodeGoLabel,
			Reporter: opencodeGoUsage(),
		},
	}
}

func anthropicUsage() agent.UsageReporter {
	if !IsLoggedIn(model.AnthropicProvider) {
		return nil
	}

	client, err := anthropic.New(
		anthropic.StoredCredentials(), listingModel, listingEffort, listingMaxOutputTokens,
	)
	if err != nil {
		return nil
	}

	return client
}

func codexUsage() agent.UsageReporter {
	if !IsLoggedIn(model.CodexProvider) {
		return nil
	}

	client, err := codex.New(codex.StoredCredentials(), listingModel, listingEffort)
	if err != nil {
		return nil
	}

	return client
}

func opencodeGoUsage() agent.UsageReporter {
	key, err := opencodego.StoredKey()
	if err != nil {
		return nil
	}

	client, err := opencodego.New(
		opencodego.EndpointURL, key, listingModel, listingEffort, listingMaxOutputTokens,
	)
	if err != nil {
		return nil
	}

	client.UsageURL = opencodego.UsageEndpointURL

	return client
}
