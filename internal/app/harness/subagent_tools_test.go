package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/toolbox/subagent"
)

const (
	cappedChildAnswer  = "Everything the parent offers was read."
	cappedParentAnswer = "The child read like its parent."
	cappedDelegated    = "A child is reading."
	probeSkillBody     = "Probe the child's view of global skills."
	listedFiles        = 400
	listedDirectory    = "listing"
	bigFileName        = "big.txt"
	bigFileLines       = 2000
)

var (
	savedOutput      = regexp.MustCompile(`full output in (\S+)\]`)
	listingArguments = encodedArguments(map[string]string{"pattern": "*.txt", "path": listedDirectory})
)

func listedFileName(index int) string {
	return fmt.Sprintf("a-file-with-a-rather-long-name-%04d.txt", index)
}

func writeOversizedWorkspace(t *testing.T, rig *interactiveRig) {
	t.Helper()

	var big strings.Builder
	for line := range bigFileLines {
		fmt.Fprintf(&big, "line %04d of a big file\n", line)
	}
	if err := os.WriteFile(filepath.Join(rig.workspace, bigFileName), []byte(big.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(rig.workspace, listedDirectory)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range listedFiles {
		if err := os.WriteFile(filepath.Join(directory, listedFileName(index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func callOutputs(request sim.Request) []sim.Entry {
	return slices.DeleteFunc(slices.Clone(request.Input), func(entry sim.Entry) bool { return entry.Type != sim.CallOutput })
}

func readCallOf(path string) sim.Call {
	return sim.Call{Name: "read", Arguments: encodedArguments(map[string]string{"path": path})}
}

func cappingResponder(skillPath *string) sim.Responder {
	start := encodedArguments(subagent.Args{Action: subagent.Start, Subagents: []subagent.Task{{
		Model:  "opencode-go/deepseek-v4.1-flash@high",
		Prompt: "Read the big file, the listing, and the probe skill",
		Intent: "Reading oversized output and a skill",
	}}})
	return func(request sim.Request) sim.Turn {
		if strings.Contains(request.Instructions, childIdentity) {
			outputs := callOutputs(request)
			switch {
			case len(outputs) == 0:
				return sim.Turn{Calls: []sim.Call{readCallOf(bigFileName), {Name: "find", Arguments: listingArguments}, readCallOf(*skillPath)}}
			case len(outputs) == 3:
				for _, output := range outputs {
					if found := savedOutput.FindStringSubmatch(output.Output); found != nil {
						return sim.Turn{Calls: []sim.Call{readCallOf(found[1])}}
					}
				}
			}
			return sim.Turn{Say: cappedChildAnswer}
		}
		switch {
		case mentions(request, cappedChildAnswer):
			return sim.Turn{Say: cappedParentAnswer}
		case hasCallOutput(request):
			return sim.Turn{Say: cappedDelegated}
		}
		return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: start}}}
	}
}

func childResultsByArguments(events []agent.Event) map[string]agent.Event {
	arguments := map[string]string{}
	results := map[string]agent.Event{}
	for _, event := range events {
		if event.Kind == agent.ToolCallRequestEvent {
			arguments[event.ID] = event.Arguments
		}
		if event.Kind == agent.ToolCallResultEvent {
			results[arguments[event.ID]] = event
		}
	}
	return results
}
