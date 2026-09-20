package codex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"crdx.org/io/pkg/agent"
)

func listModels(t *testing.T, document string) ([]agent.Model, string) {
	t.Helper()

	asked := ""

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked = request.URL.RequestURI()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, document)
	}))
	t.Cleanup(server.Close)

	client, err := New(Static("token", "account"), "first", "high")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = server.URL + "/backend-api/codex/responses"

	models, err := client.Models(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return models, asked
}

func TestModelsComeFromTheCodexCatalogue(t *testing.T) {
	models, asked := listModels(t, `{"models":[{
		"slug": "first",
		"display_name": "First",
		"visibility": "list",
		"context_window": 272000,
		"supported_reasoning_levels": [{"effort":"low"},{"effort":"high"}]
	}]}`)

	if asked != "/backend-api/codex/models?client_version="+ClientVersion {
		t.Errorf("expected the Codex catalogue to be asked for our client version, and it asked %q", asked)
	}

	if len(models) != 1 || models[0].ID != "first" || models[0].Name != "First" {
		t.Fatalf("got models %+v", models)
	}

	if !slices.Equal(models[0].EffortLevels, []string{"low", "high"}) {
		t.Errorf("got effort levels %v", models[0].EffortLevels)
	}

	if models[0].ContextWindowTokens != 272_000 {
		t.Errorf("got a context window of %d", models[0].ContextWindowTokens)
	}
}

func TestModelsCodexCannotDriveAreNotOffered(t *testing.T) {
	models, _ := listModels(t, `{"models":[
		{"slug":"first","display_name":"First","visibility":"list"},
		{"slug":"hidden","display_name":"Hidden","visibility":"hide"},
		{"slug":"withheld","display_name":"Withheld","visibility":"none"}
	]}`)

	if len(models) != 1 || models[0].ID != "first" {
		t.Fatalf("expected only the listed model to be offered, got %+v", models)
	}
}

func TestModelsLeavesUnlistedCapabilitiesUnknown(t *testing.T) {
	models, _ := listModels(t, `{"data":[{"id":"second"}]}`)

	if len(models) != 1 || models[0].ID != "second" {
		t.Fatalf("got models %+v", models)
	}

	if models[0].EffortLevels != nil || models[0].ContextWindowTokens != 0 {
		t.Errorf("the listing claimed capabilities it did not report: %+v", models)
	}
}

func TestOnlyResponsesModelsAreSupported(t *testing.T) {
	for _, modelID := range []string{
		"gpt-audio-2",
		"text-embedding-4",
		"gpt-image-2",
		"omni-moderation-2",
		"gpt-realtime-2.1",
		"gpt-transcribe-2",
		"gpt-tts-2",
	} {
		if SupportsResponses(modelID) {
			t.Errorf("expected %s not to support Responses", modelID)
		}
	}

	for _, modelID := range []string{"gpt-5.6-sol", "gpt-5.3-codex", "o3"} {
		if !SupportsResponses(modelID) {
			t.Errorf("expected %s to support Responses", modelID)
		}
	}
}
