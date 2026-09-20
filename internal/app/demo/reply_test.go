package demo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/sim"
)

func userTurn(said string) sim.Request {
	return sim.Request{Input: []sim.Entry{{Type: sim.Message, Role: "user", Content: said}}}
}

func TestTheSimulationReachesForTheToolTheWordsName(t *testing.T) {
	tests := map[string]struct {
		said     string
		toolName string
		wants    map[string]string
	}{
		"a named path":        {said: "read main.go", toolName: readTool, wants: map[string]string{"path": "main.go"}},
		"a quoted path":       {said: "show me `internal/sim/endpoint.go` please", toolName: readTool, wants: map[string]string{"path": "internal/sim/endpoint.go"}},
		"the workspace":       {said: "what files are here?", toolName: listTool, wants: map[string]string{}},
		"a named directory":   {said: "list internal/app/demo/", toolName: listTool, wants: map[string]string{"path": "internal/app/demo/"}},
		"a word to look for":  {said: "search for wizard", toolName: grepTool, wants: map[string]string{"pattern": "wizard"}},
		"a quoted expression": {said: `grep "castSpell" everywhere`, toolName: grepTool, wants: map[string]string{"pattern": "castSpell"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			turn := answer(userTurn(test.said))

			if len(turn.Calls) != 1 {
				t.Fatalf("%q asked for %d tools", test.said, len(turn.Calls))
			}
			if turn.Calls[0].Name != test.toolName {
				t.Errorf("%q reached for %q, want %q", test.said, turn.Calls[0].Name, test.toolName)
			}

			var asked map[string]string
			if err := json.Unmarshal([]byte(turn.Calls[0].Arguments), &asked); err != nil {
				t.Fatal(err)
			}
			if len(asked) != len(test.wants) {
				t.Fatalf("%q asked with %v, want %v", test.said, asked, test.wants)
			}
			for key, want := range test.wants {
				if asked[key] != want {
					t.Errorf("%q asked for %s %q, want %q", test.said, key, asked[key], want)
				}
			}
		})
	}
}

func TestTheSimulationNeverReachesForAToolItIsNotOffered(t *testing.T) {
	said := []string{
		"hello", "what can you do", "read main.go", "what files are here", "search for wizard",
		"write me a file", "run ls for me", "bash echo hello", "edit main.go", "expose port 8080",
		"start a job", "delete everything", "show off", "I am unhappy", "",
	}

	for _, message := range said {
		for _, call := range answer(userTurn(message)).Calls {
			if !slices.Contains(Tools(), call.Name) {
				t.Errorf("%q reached for the %s tool, which the simulation is not offered", message, call.Name)
			}
		}
	}
}

func TestTheSimulationSaysWhatItIsWhenAsked(t *testing.T) {
	for _, said := range []string{"hello", "hi there", "what are you?", "are you an llm"} {
		turn := answer(userTurn(said))

		if len(turn.Calls) != 0 {
			t.Errorf("%q reached for a tool", said)
		}
		if !strings.Contains(turn.Say, "simulation") {
			t.Errorf("%q was answered with %q", said, turn.Say)
		}
	}
}

func TestTheSimulationAdmitsWhatItCannotDo(t *testing.T) {
	for _, said := range []string{"write a test for this", "fix the bug in the parser", "run the tests"} {
		turn := answer(userTurn(said))

		if len(turn.Calls) != 0 {
			t.Errorf("%q reached for a tool", said)
		}
		if !strings.Contains(turn.Say, "only read, list, and search") {
			t.Errorf("%q was answered with %q", said, turn.Say)
		}
	}
}

func TestTheSimulationAnswersTheWorkThatCameBack(t *testing.T) {
	tests := map[string]struct {
		toolName string
		output   string
		wants    string
	}{
		"a file":       {toolName: readTool, output: "one\ntwo\nthree\n", wants: "3 lines"},
		"a listing":    {toolName: listTool, output: "main.go\n", wants: "one entry"},
		"some matches": {toolName: grepTool, output: "a.go:1:x\nb.go:2:y\n", wants: "2 matches"},
		"no matches":   {toolName: grepTool, output: "", wants: "Nothing matched"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			turn := answer(sim.Request{Input: []sim.Entry{
				{Type: sim.Message, Role: "user", Content: "read something"},
				{Type: sim.CallMade, CallID: "call-1", Name: test.toolName},
				{Type: sim.CallOutput, CallID: "call-1", Output: test.output},
			}})

			if len(turn.Calls) != 0 {
				t.Errorf("the simulation asked for another tool rather than answering")
			}
			if !strings.Contains(turn.Say, test.wants) {
				t.Errorf("the simulation said %q, want %q in it", turn.Say, test.wants)
			}
		})
	}
}

func TestTheSimulationHandsAnUnmatchedMessageToTheDoctor(t *testing.T) {
	turn := answer(userTurn("the quarterly figures look wrong"))

	if len(turn.Calls) != 0 {
		t.Errorf("an unmatched message reached for a tool")
	}
	if turn.Say != "I am not sure I understand you fully." {
		t.Errorf("an unmatched message was answered with %q", turn.Say)
	}
}

func TestTheDoctorHearsEveryMessageTheRestOfTheSimulationLeftAlone(t *testing.T) {
	turn := answer(sim.Request{Input: []sim.Entry{
		{Type: sim.Message, Role: "user", Content: "my boyfriend made me come here"},
		{Type: sim.Message, Role: "assistant", Content: "Your boyfriend made you come here"},
		{Type: sim.Message, Role: "user", Content: "read notes.txt"},
		{Type: sim.Message, Role: "assistant", Content: "Reading `notes.txt` now."},
		{Type: sim.Message, Role: "user", Content: "bullies"},
		{Type: sim.Message, Role: "assistant", Content: "I am not sure I understand you fully"},
		{Type: sim.Message, Role: "user", Content: "bullies again"},
	}})

	if !strings.Contains(turn.Say, "your boyfriend made you come here") {
		t.Errorf("the doctor forgot what came before, and said %q", turn.Say)
	}
}

func TestTheSimulationAnswersTheLatestThingSaid(t *testing.T) {
	turn := answer(sim.Request{Input: []sim.Entry{
		{Type: sim.Message, Role: "user", Content: "hello"},
		{Type: sim.Message, Role: "assistant", Content: "Hello."},
		{Type: sim.Message, Role: "user", Content: "read notes.txt"},
	}})

	if len(turn.Calls) != 1 || turn.Calls[0].Name != readTool {
		t.Fatalf("the simulation answered the wrong message with %+v", turn)
	}
	if !strings.Contains(turn.Calls[0].Arguments, "notes.txt") {
		t.Errorf("the simulation read %q", turn.Calls[0].Arguments)
	}
}

func TestTheSimulationTakesUpTheNextMessageAfterAToolHasAnswered(t *testing.T) {
	turn := answer(sim.Request{Input: []sim.Entry{
		{Type: sim.Message, Role: "user", Content: "what files are here"},
		{Type: sim.CallMade, CallID: "call-1", Name: listTool},
		{Type: sim.CallOutput, CallID: "call-1", Output: "notes.txt\n"},
		{Type: sim.Message, Role: "assistant", Content: "There is one entry there."},
		{Type: sim.Message, Role: "user", Content: "read notes.txt"},
	}})

	if len(turn.Calls) != 1 || turn.Calls[0].Name != readTool {
		t.Fatalf("the simulation answered the finished tool call again with %+v", turn)
	}
}
