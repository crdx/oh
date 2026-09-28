package tool_test

import (
	"context"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool"
)

func revisionedWeather(revision string, description string, schema tool.Schema) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: "weather", Description: description, Schema: schema},
		func(struct{}) tool.CallRendering { return tool.CallRendering{} },
	).Revision(revision).Plain(func(context.Context, struct{}) (string, error) {
		return "sunny", nil
	})
}

func TestCompatibilityIsDeclaredByTheRevision(t *testing.T) {
	original := revisionedWeather("2", "report weather", tool.Schema{tool.String("city", "the city")})
	snapshot := tool.TakeSnapshot(original)

	for name, candidate := range map[string]tool.Tool{
		"same":        revisionedWeather("2", "report weather", tool.Schema{tool.String("city", "the city")}),
		"revision":    revisionedWeather("3", "report weather", tool.Schema{tool.String("city", "the city")}),
		"description": revisionedWeather("2", "forecast weather", tool.Schema{tool.String("city", "a place")}),
		"schema":      revisionedWeather("2", "report weather", tool.Schema{tool.String("place", "the city")}),
	} {
		isWanted := name != "revision"
		if got := tool.IsCompatible(candidate, snapshot); got != isWanted {
			t.Errorf("%s: got compatibility %v, want %v", name, got, isWanted)
		}
	}
}

func TestASnapshotKeepsItsDefinitionWhileDelegatingToTheCurrentTool(t *testing.T) {
	original := revisionedWeather("2", "report weather", tool.Schema{tool.String("city", "the city")})
	current := revisionedWeather("2", "forecast weather", tool.Schema{tool.String("city", "a place")})
	bound := tool.WithSnapshot(current, tool.TakeSnapshot(original))

	if bound.Description() != "report weather" || bound.Schema()[0].Description != "the city" {
		t.Errorf("got definition %#v", tool.Describe(bound))
	}
	call, err := bound.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "sunny" {
		t.Errorf("got output %q", result.Output)
	}
}

func TestAnUnavailableToolKeepsItsFrozenContractAndRefusesExecution(t *testing.T) {
	original := revisionedWeather("2", "report weather", tool.Schema{tool.String("city", "the city")})
	unavailable := tool.Unavailable(tool.TakeSnapshot(original), "weather changed")

	if !tool.IsCompatible(unavailable, tool.TakeSnapshot(original)) {
		t.Fatal("the unavailable tool did not preserve its contract")
	}
	call, err := unavailable.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatal(err)
	}
	if call.Rendering().Subject != "London" {
		t.Errorf("got subject %q", call.Rendering().Subject)
	}
	if _, err := call.Exec(t.Context()); err == nil || !strings.Contains(err.Error(), "weather changed") {
		t.Errorf("got %v", err)
	}
}
