package responses

import (
	"context"
	"strings"

	"crdx.org/oh/pkg/agent"
)

const responsesSuffix = "/responses"

const modelsSuffix = "/models"

const ClientVersion = "0.153.4"

const listedVisibility = "list"

func SupportsResponses(id string) bool {
	for segment := range strings.SplitSeq(id, "-") {
		switch segment {
		case "audio", "embedding", "image", "moderation", "realtime", "transcribe", "tts":
			return false
		}
	}

	return true
}

type reasoningLevel struct {
	Effort string `json:"effort"`
}

type listedModel struct {
	Slug          string           `json:"slug"`
	DisplayName   string           `json:"display_name"`
	Visibility    string           `json:"visibility"`
	ContextWindow int              `json:"context_window"`
	Levels        []reasoningLevel `json:"supported_reasoning_levels"`
}

func (self listedModel) isPickable() bool {
	return self.Slug != "" && (self.Visibility == "" || self.Visibility == listedVisibility)
}

func (self listedModel) efforts() []string {
	if len(self.Levels) == 0 {
		return nil
	}

	efforts := make([]string, 0, len(self.Levels))

	for _, level := range self.Levels {
		if level.Effort != "" {
			efforts = append(efforts, level.Effort)
		}
	}

	if len(efforts) == 0 {
		return nil
	}

	return efforts
}

func (self *Client) Models(ctx context.Context) ([]agent.Model, error) {
	address, listable := modelsAddress(self.URL)
	if !listable {
		return nil, nil
	}

	token, err := self.tokens.Token()
	if err != nil {
		return nil, err
	}

	var payload struct {
		Models []listedModel `json:"models"`

		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	address += "?client_version=" + ClientVersion
	if err := self.observedRequests().Get(ctx, address, self.headers(token), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Models)+len(payload.Data))

	for _, model := range payload.Models {
		if model.isPickable() {
			models = append(models, agent.Model{
				ID:                  model.Slug,
				Name:                model.DisplayName,
				EffortLevels:        model.efforts(),
				ContextWindowTokens: model.ContextWindow,
			})
		}
	}

	for _, model := range payload.Data {
		if model.ID != "" {
			models = append(models, agent.Model{ID: model.ID})
		}
	}

	return models, nil
}

func modelsAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, responsesSuffix)
	if !found {
		return "", false
	}

	return prefix + modelsSuffix, true
}
