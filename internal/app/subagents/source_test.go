package subagents

import (
	"context"
	"encoding/json"
	"testing"

	"crdx.org/oh/pkg/toolbox/wait"
)

func waitOn(t *testing.T, manager *Manager, until string, seconds int, names ...string) string {
	t.Helper()

	arguments, err := json.Marshal(wait.Args{Names: names, Until: until, Seconds: seconds})
	if err != nil {
		t.Fatal(err)
	}
	built := wait.New([]wait.Source{manager.WaitSource()}, func(context.Context) <-chan struct{} { return nil })
	parsed, err := built.Parse(string(arguments))
	if err != nil {
		t.Fatal(err)
	}
	result, err := parsed.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return result.Output
}
