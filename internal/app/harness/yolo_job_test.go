package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/sim"
)

func TestAJobRunsOnTheHostOutsideTheSandbox(t *testing.T) {
	rig := newScriptedRig(t,
		sim.Turn{Calls: []sim.Call{{
			Name:      "job",
			Arguments: `{"action":"start","name":"check","command":"echo from-the-$((1+1))-host","intent":"Checking a job runs on the host"}`,
		}}},
		sim.Turn{Calls: []sim.Call{{
			Name:      "job",
			Arguments: `{"action":"wait","names":["check"]}`,
		}}},
		sim.Turn{Say: "The job ran."},
	)
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-cx", "-m", "opencode-go/fake", "run a job")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	journal, err := os.ReadFile(filepath.Join(
		rig.stateDirectory, "org.crdx", "oh", "sessions", storedSessions[0].Name, "session.jsonl",
	))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(journal), "from-the-2-host") {
		t.Errorf("the job's output never reached the journal:\n%s", journal)
	}
	if strings.Contains(string(journal), "unknown tool") {
		t.Errorf("the job tool was not offered under --yolo:\n%s", journal)
	}
}
