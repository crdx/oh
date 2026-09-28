package store_test

import (
	"context"
	"testing"

	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/tool"
)

func snapshottedTool() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema: tool.Schema{
				tool.String("city", "the city to look up"),
				tool.Integer("days", "how many days ahead").Optional(),
				tool.Enum("unit", "the unit to report in", "celsius", "fahrenheit").Optional(),
				tool.StringArray("layers", "the layers to include").Optional(),
				tool.Boolean("detailed", "whether to include every hour"),
			},
		},
		func(struct{}) tool.CallRendering { return tool.CallRendering{} },
	).Revision("weather/v3").Plain(func(context.Context, struct{}) (string, error) {
		return "", nil
	})
}

func TestFrozenToolsRestoreTheirWholeContract(t *testing.T) {
	original := snapshottedTool()
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{ToolDefinitions: store.FreezeTools([]tool.Tool{original})})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	restored := store.RestoreTools(storedSession.Meta.ToolDefinitions)
	if len(restored) != 1 {
		t.Fatalf("got %d restored tools", len(restored))
	}
	if !tool.IsCompatible(original, restored[0]) {
		t.Error("the restored contract is not compatible with its source")
	}
}

func TestASessionWithoutFrozenToolsRestoresNone(t *testing.T) {
	if snapshots := store.RestoreTools(nil); len(snapshots) != 0 {
		t.Errorf("got %v, want no snapshots", snapshots)
	}
}
