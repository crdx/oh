package call_test

import (
	"context"
	"testing"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type echoArgs struct {
	Text string `json:"text"`
}

func countingEcho(renderings *int) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: "echo", Description: "echo", Schema: tool.Schema{tool.String("text", "what to say")}},
		func(args echoArgs) tool.CallRendering {
			*renderings++
			return tool.CallRendering{Subject: args.Text, Mentions: []string{"subagent:" + args.Text}}
		},
	).Plain(func(context.Context, echoArgs) (string, error) { return "", nil })
}

func lookupOnly(known tool.Tool) call.ToolLookup {
	return func(name string) (tool.Tool, bool) {
		if name == known.Name() {
			return known, true
		}
		return nil, false
	}
}

func TestADescriberDescribesEachDistinctCallOnce(t *testing.T) {
	renderings := 0
	describer := call.NewDescriber(lookupOnly(countingEcho(&renderings)), nil)
	hello := agent.Event{ID: "first", Name: "echo", Arguments: `{"text":"hello"}`}
	again := agent.Event{ID: "second", Name: "echo", Arguments: `{"text":"hello"}`}
	other := agent.Event{ID: "third", Name: "echo", Arguments: `{"text":"goodbye"}`}

	for range 3 {
		describer.LabelFor(hello)
	}
	describer.LabelFor(again)
	if renderings != 1 {
		t.Errorf("got %d renderings, want one for a call drawn four times", renderings)
	}

	describer.LabelFor(other)
	if renderings != 2 {
		t.Errorf("got %d renderings, want a second for different arguments", renderings)
	}
}

func TestADescribedCallDrawsAsItsUncachedLabel(t *testing.T) {
	renderings := 0
	getTool := lookupOnly(countingEcho(&renderings))
	workspace := work.At("/home/someone/project")
	describer := call.NewDescriber(getTool, workspace)

	for name, event := range map[string]agent.Event{
		"known":    {Name: "echo", Arguments: `{"text":"/home/someone/project/main.go"}`},
		"unparsed": {Name: "echo", Arguments: `{"text":`},
		"unknown": {
			Name:              "vanished",
			Arguments:         `{"path":"/home/someone/project/notes.md"}`,
			FallbackRendering: agent.FallbackRendering{Subject: "/home/someone/project/notes.md"},
		},
	} {
		want := call.LabelFor(event, getTool, workspace)
		for draw := range 2 {
			got := describer.LabelFor(event)
			if got.Render() != want.Render() {
				t.Errorf("%s draw %d: got %q, want %q", name, draw, got.Render(), want.Render())
			}
			if got.TimeLimit != want.TimeLimit || got.Intent != want.Intent || len(got.Mentions) != len(want.Mentions) {
				t.Errorf("%s draw %d: got %+v, want %+v", name, draw, got, want)
			}
		}
	}
}

func TestChangingADescribedLabelLeavesTheNextDrawAlone(t *testing.T) {
	renderings := 0
	describer := call.NewDescriber(lookupOnly(countingEcho(&renderings)), nil)
	event := agent.Event{Name: "echo", Arguments: `{"text":"hello"}`}

	first := describer.LabelFor(event)
	first.Mentions[0] = "changed"

	if got := describer.LabelFor(event).Mentions; len(got) != 1 || got[0] != "subagent:hello" {
		t.Errorf("got mentions %q, want the described call untouched", got)
	}
}
