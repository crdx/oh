package backend

import (
	"context"
	"fmt"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/anthropic"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/provider/opencodego"
	"crdx.org/oh/pkg/tool"

	"crdx.org/oh/internal/app/model"
)

const EndpointVariable = "OH_ENDPOINT_URL"

type EndpointSettings struct {
	OverrideURL string
	OllamaHost  string
}

const (
	standInToken           = "stand-in"
	listingModel           = "listing"
	listingEffort          = "high"
	listingMaxOutputTokens = 1
)

type Client interface {
	agent.Provider
	agent.State
	ObserveHTTP(observer req.Observer)
}

type Connection struct {
	Client

	ToolsSize func([]tool.Tool) int

	Search *codex.SearchClient
}

func (self *Connection) ObserveHTTP(observer req.Observer) {
	self.Client.ObserveHTTP(observer)
	self.Search.ObserveHTTP(observer)
}

type sessionScoped interface {
	UseSession(id string)
}

func (self *Connection) UseSession(id string) {
	if scopedClient, isScoped := self.Client.(sessionScoped); isScoped {
		scopedClient.UseSession(id)
	}
}

func Connect(choice model.Choice, selection model.Selection, endpoints EndpointSettings) (*Connection, error) {
	if selection.IsFast && !model.SupportsFastMode(choice.Provider) {
		return nil, fmt.Errorf("%s does not support fast mode", choice.Provider)
	}
	if err := requireCredentials(choice.Provider, endpoints.OverrideURL); err != nil {
		return nil, err
	}

	connection, err := connectProvider(choice, selection, endpoints)
	if err != nil {
		return nil, err
	}

	if connection.Search == nil {
		tokens, address := codexCredentials(endpoints.OverrideURL)
		if connection.Search, err = newSearchClient(tokens, address); err != nil {
			return nil, err
		}
	}

	return connection, nil
}

func IsLoggedIn(providerName string) bool {
	return requireCredentials(providerName, "") == nil
}

func IsAvailable(providerName string, endpoints EndpointSettings) bool {
	return requireCredentials(providerName, endpoints.OverrideURL) == nil
}

func requireCredentials(providerName string, overrideURL string) error {
	if overrideURL != "" {
		return nil
	}

	switch providerName {
	case model.CodexProvider:
		_, err := codex.LoadStoredCredentials()

		return err
	case model.AnthropicProvider:
		_, err := anthropic.LoadStoredCredentials()

		return err
	case model.OpencodeGoProvider:
		_, err := opencodego.StoredKey()

		return err
	default:
		return nil
	}
}

func connectProvider(choice model.Choice, selection model.Selection, endpoints EndpointSettings) (*Connection, error) {
	switch choice.Provider {
	case model.CodexProvider:
		return connectCodex(choice, selection, endpoints.OverrideURL)
	case model.OpencodeGoProvider:
		return connectOpencodeGo(choice, selection.Effort, endpoints.OverrideURL)
	case model.AnthropicProvider:
		return connectAnthropic(choice, selection.Effort, endpoints.OverrideURL)
	case model.OllamaProvider:
		return connectOllama(choice, selection.Effort, endpoints)
	default:
		return nil, fmt.Errorf("unknown provider %q", choice.Provider)
	}
}

func ListModels(ctx context.Context, providerName string, endpoints EndpointSettings) ([]agent.Model, error) {
	choice := model.Choice{
		Provider:        providerName,
		ID:              listingModel,
		MaxOutputTokens: listingMaxOutputTokens,
	}
	selection := model.Selection{Provider: providerName, Model: listingModel, Effort: listingEffort}
	client, err := connectProvider(choice, selection, endpoints)
	if err != nil {
		return nil, err
	}

	lister, canList := client.Client.(agent.Lister)
	if !canList {
		return nil, agent.ErrNoListing
	}

	return lister.Models(ctx)
}

func Resolve(
	requestedSelection model.Selection,
	resumedSelection model.Selection,
	configuredSelections []model.Selection,
	roundRobinPath string,
) (model.Selection, error) {
	return ResolveAvailable(
		requestedSelection,
		resumedSelection,
		configuredSelections,
		roundRobinPath,
		func(model.Selection) bool { return true },
	)
}

func ResolveAvailable(
	requestedSelection model.Selection,
	resumedSelection model.Selection,
	configuredSelections []model.Selection,
	roundRobinPath string,
	isAvailable func(model.Selection) bool,
) (model.Selection, error) {
	if resumedSelection != (model.Selection{}) {
		if requestedSelection != (model.Selection{}) && requestedSelection != resumedSelection {
			return model.Selection{}, fmt.Errorf(
				"cannot resume a conversation held with %s under %s: a model is chosen when a session is created",
				resumedSelection, requestedSelection,
			)
		}

		return resumedSelection, nil
	}

	if requestedSelection.Model != "" {
		return requestedSelection, nil
	}

	return model.ReserveAvailableRoundRobin(roundRobinPath, configuredSelections, isAvailable)
}
