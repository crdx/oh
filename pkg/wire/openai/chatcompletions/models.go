package chatcompletions

import (
	"context"
	"strings"

	"crdx.org/oh/pkg/agent"
)

const completionsSuffix = "/chat/completions"

const modelsSuffix = "/models"

func (self *Client) Models(ctx context.Context) ([]agent.Model, error) {
	address, listable := modelsAddress(self.URL)
	if !listable {
		return nil, nil
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := self.observedRequests().Get(ctx, address, self.headers("application/json"), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Data))

	for _, listedModel := range payload.Data {
		if listedModel.ID != "" {
			models = append(models, agent.Model{ID: listedModel.ID})
		}
	}

	return models, nil
}

func modelsAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, completionsSuffix)
	if !found {
		return "", false
	}

	return prefix + modelsSuffix, true
}
