package toolset

import (
	"context"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/pkg/tool"
)

type fakeArgs struct {
	Path string `json:"path"`
}

func namedTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	).Plain(func(context.Context, fakeArgs) (string, error) {
		return "", nil
	})
}

func namesOf(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, enabledTool := range tools {
		names = append(names, enabledTool.Name())
	}

	return names
}

func TestOnlyNamedToolsAreEnabled(t *testing.T) {
	availableTools := []tool.Tool{namedTool("read"), namedTool("grep"), namedTool("write")}

	allTools, err := Reduce(availableTools, nil)
	if err != nil || len(allTools) != len(availableTools) {
		t.Fatalf("expected every tool by default, got %v, %v", allTools, err)
	}

	enabledTools, err := Reduce(availableTools, []string{"write", "read", "read"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if names := namesOf(enabledTools); !slices.Equal(names, []string{"read", "write"}) {
		t.Errorf("expected read and write in canonical order, got %v", names)
	}

	if _, err := Reduce(availableTools, []string{"gone"}); err == nil {
		t.Error("expected an unavailable tool to be rejected")
	}
}

func TestEveryUnavailableToolIsNamedAtOnce(t *testing.T) {
	availableTools := []tool.Tool{namedTool("read")}

	_, err := Reduce(availableTools, []string{"gone", "read", "missing"})
	if err == nil || !strings.Contains(err.Error(), "tools not available: gone, missing") {
		t.Fatalf("expected both to be named, got %v", err)
	}
}

func TestTheToolsOfAConversationCanBeNamedAndReducedBackToThemselves(t *testing.T) {
	offered := []tool.Tool{namedTool("Read"), namedTool("Bash"), namedTool("job")}

	names := Names(offered)
	if !slices.Equal(names, []string{"Read", "Bash", "job"}) {
		t.Fatalf("got %v", names)
	}

	restored, err := Reduce(offered, names)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(Names(restored), names) {
		t.Errorf("got %v, want %v", Names(restored), names)
	}
}

func TestAConversationOpensWithoutTheToolsThatHaveGoneAway(t *testing.T) {
	held := Names([]tool.Tool{namedTool("Read"), namedTool("job"), namedTool("Bash")})

	present, absent := Partition([]tool.Tool{namedTool("Read"), namedTool("Bash")}, held)
	if !slices.Equal(present, []string{"Read", "Bash"}) {
		t.Errorf("got %v", present)
	}
	if !slices.Equal(absent, []string{"job"}) {
		t.Errorf("got %v", absent)
	}

	if _, err := Reduce([]tool.Tool{namedTool("Read"), namedTool("Bash")}, present); err != nil {
		t.Fatal(err)
	}
}

func TestEveryToolStillThereIsKeptInTheOrderItWasHeld(t *testing.T) {
	held := Names([]tool.Tool{namedTool("Read"), namedTool("Bash")})

	present, absent := Partition([]tool.Tool{namedTool("Bash"), namedTool("Read")}, held)
	if !slices.Equal(present, held) || absent != nil {
		t.Errorf("got %v and %v, want %v and nothing", present, absent, held)
	}
}
