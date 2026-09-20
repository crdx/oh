package sim_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/sim"
)

func TestEveryScenarioThatShipsCanBeRead(t *testing.T) {
	paths, err := filepath.Glob("scenarios/*.toml")
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected the scenarios to be found")
	}

	for _, path := range paths {
		scenario, err := sim.Read(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}

		for i, turn := range scenario.Turns {
			if turn.ErrorEvent != "" && !json.Valid([]byte(turn.ErrorEvent)) {
				t.Errorf("%s: turn %d has an invalid error event", path, i+1)
			}

			for _, call := range turn.Calls {
				if call.Name == "" {
					t.Errorf("%s: turn %d asks for a tool with no name", path, i+1)
				}

				var arguments map[string]any
				if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
					t.Errorf("%s: turn %d: %s: %v", path, i+1, call.Name, err)
				}
			}
		}
	}
}
