package harness

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/toolbox/subagent"
)

const (
	delegatingPrompt   = "delegate the review"
	delegatedAnswer    = "Two subagents are on it."
	followUp           = "Now quote it twice"
	askedAgainAnswer   = "Asked again."
	followedUpAnswer   = "Both followed through."
	firstChildAnswer   = "The README says hello."
	secondChildAnswer  = "There are two TODO items."
	followUpChildReply = "hello hello"
	scratchProbe       = "scratch-is-private"
	childIdentity      = "Your identity is "
	secondWorkspace    = "docs"
)

const fakePager = `#!/bin/sh
for last; do :; done
printf '\033[?1049h\033[H'
sed 's/$/\r/' "$last"
IFS= read -r line
printf '\033[?1049l'
`

func withFakePager(t *testing.T, rig *interactiveRig) {
	t.Helper()

	directory := t.TempDir()
	//nolint:gosec // the pager has to be executable
	if err := os.WriteFile(filepath.Join(directory, "less"), []byte(fakePager), 0o755); err != nil {
		t.Fatal(err)
	}
	rig.environment = append(rig.environment, "PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

var (
	childScratch = regexp.MustCompile(`at (\S+) on the host|TMPDIR is (\S+?)\.(?:\s|$)`)
	readmeReader = regexp.MustCompile(`(\S+) done(?: \([^)]*\))?: ` + regexp.QuoteMeta(firstChildAnswer))
)

func newRespondingRig(t *testing.T, respond sim.Responder) (*interactiveRig, *sim.Endpoint) {
	t.Helper()

	endpoint := sim.NewResponder(&sim.Scenario{Model: "fake"}, respond)
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	stateDirectory := reachableWorkspaceDir(t)
	return &interactiveRig{
		t:              t,
		binary:         buildTestBinary(t),
		workspace:      reachableWorkspaceDir(t),
		stateDirectory: stateDirectory,
		environment: append(
			interactiveEnvironment(t, stateDirectory),
			backend.EndpointVariable+"="+endpoint.Addresses(server.URL)[sim.Completions],
			"TERM=xterm-256color",
		),
	}, endpoint
}

func encodedArguments(arguments any) string {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func hasCallOutput(request sim.Request) bool {
	return slices.ContainsFunc(request.Input, func(entry sim.Entry) bool { return entry.Type == sim.CallOutput })
}

func mentions(request sim.Request, text string) bool {
	return slices.ContainsFunc(request.Input, func(entry sim.Entry) bool {
		return strings.Contains(entry.Content, text) || strings.Contains(entry.Output, text)
	})
}

func readmeReaderIn(request sim.Request) string {
	for _, entry := range request.Input {
		if found := readmeReader.FindStringSubmatch(entry.Content); found != nil {
			return found[1]
		}
	}
	return ""
}

func delegatingResponder(firstChildCall sim.Call) sim.Responder {
	start := encodedArguments(subagent.Args{Action: subagent.Start, Subagents: []subagent.Task{
		{Prompt: "Read the README and quote it", Intent: "Quoting what the README says"},
		{Prompt: "Count the TODO items", Intent: "Counting the TODO items in docs", Workspace: secondWorkspace},
	}})
	return func(request sim.Request) sim.Turn {
		isChild := strings.Contains(request.Instructions, childIdentity)
		switch {
		case isChild && mentions(request, followUp):
			return sim.Turn{Say: followUpChildReply}
		case isChild && mentions(request, "Read the README"):
			if !hasCallOutput(request) {
				return sim.Turn{Calls: []sim.Call{firstChildCall}}
			}
			return sim.Turn{Say: firstChildAnswer}
		case isChild:
			return sim.Turn{Say: secondChildAnswer}
		case mentions(request, followUpChildReply):
			return sim.Turn{Say: followedUpAnswer}
		case mentions(request, "sent to "):
			return sim.Turn{Say: askedAgainAnswer}
		case mentions(request, firstChildAnswer):
			send := encodedArguments(subagent.Args{Action: subagent.Send, Name: readmeReaderIn(request), Message: followUp})
			return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: send}}}
		case hasCallOutput(request):
			return sim.Turn{Say: delegatedAnswer}
		}
		return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: start}}}
	}
}

func writeSubagentWorkspace(t *testing.T, rig *interactiveRig) {
	t.Helper()

	writeRigConfig(t, rig, "[subagent]\nmodel = \"opencode-go/fake\"\n")
	if err := os.WriteFile(filepath.Join(rig.workspace, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(rig.workspace, secondWorkspace), 0o700); err != nil {
		t.Fatal(err)
	}
}

type storedFamily struct {
	parent   *store.Session
	children map[string]*store.Session
	order    []string
}

func (self storedFamily) childWithTask(t *testing.T, task string) *store.Session {
	t.Helper()

	for _, event := range self.parent.Events {
		if event.Kind == subagentrecord.Started && strings.HasPrefix(event.Text, task) {
			return self.children[event.Subagent]
		}
	}
	t.Fatalf("no child was started with %q", task)
	return nil
}

func onlyStoredFamily(t *testing.T, rig *interactiveRig) storedFamily {
	t.Helper()

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one with its children beneath it", len(storedSessions))
	}
	family := storedFamily{parent: storedSessions[0], children: map[string]*store.Session{}}
	childrenDirectory := session.ChildrenDir(filepath.Join(rig.stateDirectory, "org.crdx", "oh", "sessions"), family.parent.Name)
	for _, event := range family.parent.Events {
		if event.Kind != subagentrecord.Started {
			continue
		}
		if event.Subagent == "" || !strings.HasPrefix(event.Subagent, strings.SplitN(family.parent.Name, "-", 2)[0]+"-") {
			t.Errorf("child %q does not share its parent's adjective", event.Subagent)
		}
		child, err := store.Read(childrenDirectory, event.Subagent)
		if err != nil {
			t.Fatal(err)
		}
		if child.ID != event.ID {
			t.Errorf("%s is stored as session %s, but was started as %s", event.Subagent, child.ID, event.ID)
		}
		family.children[event.Subagent] = child
		family.order = append(family.order, event.Subagent)
	}
	return family
}

func parentFacts(events []agent.Event, name string, kind agent.Kind) []agent.Event {
	return slices.DeleteFunc(slices.Clone(events), func(event agent.Event) bool {
		return event.Subagent != name || event.Kind != kind
	})
}

func childEventsOf(child *store.Session, kind agent.Kind) []agent.Event {
	return slices.DeleteFunc(slices.Clone(child.Events), func(event agent.Event) bool { return event.Kind != kind })
}

func requireChildScratchKept(t *testing.T, child *store.Session) {
	t.Helper()

	found := childScratch.FindStringSubmatch(child.Meta.SystemPrompt)
	if found == nil {
		t.Fatalf("the child prompt names no scratch: %q", child.Meta.SystemPrompt)
	}
	scratch := found[1] + found[2]
	if !strings.Contains(scratch, filepath.Join(subagents.ScratchName, child.Name)) {
		t.Fatalf("the child scratch %q is not its own", scratch)
	}
	if info, err := os.Stat(scratch); err != nil || !info.IsDir() {
		t.Errorf("the child scratch %s did not outlive the child: %v", scratch, err)
	}
}

func TestSubagentsReportBackThroughTheBinaryAndAreListed(t *testing.T) {
	readCall := encodedArguments(map[string]string{"path": "README.md"})
	rig, endpoint := newRespondingRig(t, delegatingResponder(sim.Call{Name: "read", Arguments: readCall}))
	writeSubagentWorkspace(t, rig)
	withFakePager(t, rig)

	session := rig.start("--yolo", "-m", "opencode-go/fake", delegatingPrompt)
	session.waitFor(delegatedAnswer)
	session.waitFor(firstChildAnswer)
	session.waitFor(followedUpAnswer)
	session.requireShown(secondChildAnswer)

	session.typeText("/subs" + pressEnter)
	session.waitFor("Subagents:")
	session.typeAndSettle(pressEscape)
	session.quit()

	family := onlyStoredFamily(t, rig)
	reader := family.childWithTask(t, "Read the README")
	counter := family.childWithTask(t, "Count the TODO")
	for _, name := range family.order {
		finished := parentFacts(family.parent.Events, name, subagentrecord.Finished)
		if len(finished) == 0 || finished[len(finished)-1].Name != string(subagentrecord.Done) {
			t.Errorf("%s finished %+v, want done", name, finished)
		}
	}
	if reads := childEventsOf(reader, agent.ToolCallResultEvent); len(reads) != 1 || reads[0].Status != agent.SuccessStatus {
		t.Errorf("%s read %+v, want one successful read", reader.Name, reads)
	}
	if answers := childEventsOf(reader, agent.ModelMessageEvent); len(answers) != 2 || answers[1].Text != followUpChildReply {
		t.Errorf("%s answered %+v, want its answer and then its follow-up", reader.Name, answers)
	}
	if want := filepath.Join(rig.workspace, secondWorkspace); counter.Meta.WorkspaceDir != want {
		t.Errorf("%s worked in %s, want %s", counter.Name, counter.Meta.WorkspaceDir, want)
	}
	for _, event := range family.parent.Events {
		if event.Kind == agent.ToolCallRequestEvent && event.Name != subagent.Name {
			t.Errorf("a child's %s call reached the parent's journal", event.Name)
		}
	}
	deliveries := slices.DeleteFunc(slices.Clone(family.parent.Events), func(event agent.Event) bool {
		return event.Kind != subagentrecord.ReportsDelivered
	})
	if len(deliveries) != 2 || len(strings.Split(deliveries[0].Name, ",")) != 2 || deliveries[1].Name != reader.Name {
		t.Errorf("delivered %+v, want both children in one batch and then the follow-up", deliveries)
	}
	requireChildScratchKept(t, reader)

	asked := len(endpoint.Requests())
	resumed := rig.start("-r", family.parent.Name)
	resumed.waitFor(followedUpAnswer)
	resumed.waitToSettle()
	resumed.requireShown(secondChildAnswer)
	resumed.typeText("/sub cat " + reader.Name + pressEnter)
	resumed.waitFor(followUp)
	resumed.requireShown(followUpChildReply)
	resumed.typeText("q" + pressEnter)
	resumed.waitFor(leftTheEditor)
	resumed.waitToSettle()
	resumed.requireShown(followedUpAnswer)
	resumed.requireHidden(followUp)
	resumed.quit()
	if got := len(endpoint.Requests()); got != asked {
		t.Errorf("resuming asked the model %d more times, want delivered completions left alone", got-asked)
	}
}

func TestAConfinedSubagentKeepsItsScratchPrivateAndTheWorkspaceReadOnly(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("this machine cannot confine a subagent: %v", err)
	}
	probe := "oh-subagent-probe-" + strconv.Itoa(os.Getpid())
	command := "printf " + scratchProbe + " > /tmp/" + probe + " && cat /tmp/" + probe +
		"; echo changed >> README.md || echo refused; : > created.txt || echo refused"
	bashCall := encodedArguments(map[string]string{"command": command, "intent": "Probing the confined child scratch"})
	rig, _ := newRespondingRig(t, delegatingResponder(sim.Call{Name: "bash", Arguments: bashCall}))
	writeSubagentWorkspace(t, rig)

	session := rig.start("-c", "rx", "-m", "opencode-go/fake", delegatingPrompt)
	session.waitFor(followedUpAnswer)
	session.quit()

	reader := onlyStoredFamily(t, rig).childWithTask(t, "Read the README")
	results := childEventsOf(reader, agent.ToolCallResultEvent)
	if len(results) != 1 {
		t.Fatalf("%s ran %+v, want one command", reader.Name, results)
	}
	if !strings.Contains(results[0].Text, scratchProbe) || strings.Count(results[0].Text, "refused") != 2 {
		t.Errorf("the confined child saw %q, want its own scratch and a refused workspace write", results[0].Text)
	}
	if contents, err := os.ReadFile(filepath.Join(rig.workspace, "README.md")); err != nil || string(contents) != "hello\n" {
		t.Errorf("the confined child changed the workspace: %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(rig.workspace, "created.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the confined child created a file in the workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join("/tmp", probe)); err == nil {
		t.Error("the confined child wrote into the host's /tmp")
	}
	requireChildScratchKept(t, reader)
}

const (
	preparedNotes   = "prepared by the parent"
	preparedStarted = "The reader is on it."
	preparedRead    = "The notes are read."
	preparedDone    = "The reader finished."
)

func preparingResponder() sim.Responder {
	prepare := encodedArguments(map[string]string{
		"command": "mkdir -p /tmp/job && printf '" + preparedNotes + "' > /tmp/job/notes.txt",
		"intent":  "Preparing a folder for the subagent",
	})
	start := encodedArguments(subagent.Args{Action: subagent.Start, Subagents: []subagent.Task{
		{Prompt: "Read the prepared notes", Intent: "Reading the notes left for it", Workspace: "/tmp/job"},
	}})
	probe := encodedArguments(map[string]string{
		"command": "cat notes.txt; echo; pwd; echo changed > notes.txt || echo refused",
		"intent":  "Reading the notes the parent prepared",
	})
	return func(request sim.Request) sim.Turn {
		isChild := strings.Contains(request.Instructions, childIdentity)
		switch {
		case isChild && hasCallOutput(request):
			return sim.Turn{Say: preparedRead}
		case isChild:
			return sim.Turn{Calls: []sim.Call{{Name: "bash", Arguments: probe}}}
		case mentions(request, preparedRead):
			return sim.Turn{Say: preparedDone}
		case mentions(request, "started "):
			return sim.Turn{Say: preparedStarted}
		case hasCallOutput(request):
			return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: start}}}
		}
		return sim.Turn{Calls: []sim.Call{{Name: "bash", Arguments: prepare}}}
	}
}

func TestAConfinedSubagentReadsButNeverWritesAFolderItsParentPrepared(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("this machine cannot confine a subagent: %v", err)
	}
	rig, _ := newRespondingRig(t, preparingResponder())
	writeSubagentWorkspace(t, rig)

	session := rig.start("-c", "rx", "-m", "opencode-go/fake", "prepare a folder and hand it to a subagent")
	session.waitFor(preparedDone)
	session.quit()

	family := onlyStoredFamily(t, rig)
	reader := family.childWithTask(t, "Read the prepared notes")
	prepared := filepath.Join(rig.stateDirectory, "org.crdx", "oh", "farm", family.parent.Name, "job")
	if reader.Meta.WorkspaceDir != prepared {
		t.Errorf("%s worked in %s, want the folder its parent prepared, %s", reader.Name, reader.Meta.WorkspaceDir, prepared)
	}
	results := childEventsOf(reader, agent.ToolCallResultEvent)
	if len(results) != 1 {
		t.Fatalf("%s ran %+v, want one command", reader.Name, results)
	}
	for _, want := range []string{preparedNotes, prepared, "refused"} {
		if !strings.Contains(results[0].Text, want) {
			t.Errorf("the child saw %q, want %q in it", results[0].Text, want)
		}
	}
	if notes, err := os.ReadFile(filepath.Join(prepared, "notes.txt")); err != nil || string(notes) != preparedNotes { //nolint:gosec // the rig's own path
		t.Errorf("the child changed the folder its parent prepared: %q, %v", notes, err)
	}
	requireChildScratchKept(t, reader)
}

func TestAReadOnlySessionsSubagentsReadWithoutAShell(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("this machine cannot confine a session: %v", err)
	}
	readCall := encodedArguments(map[string]string{"path": "README.md"})
	rig, _ := newRespondingRig(t, delegatingResponder(sim.Call{Name: "read", Arguments: readCall}))
	writeSubagentWorkspace(t, rig)

	session := rig.start("-c", "r", "-m", "opencode-go/fake", delegatingPrompt)
	session.waitFor(followedUpAnswer)
	session.quit()

	family := onlyStoredFamily(t, rig)
	for _, name := range family.order {
		child := family.children[name]
		if granted, _ := caps.LastRecordedMode(child.Events); granted != caps.Read {
			t.Errorf("%s ran with %q, want its parent's read access alone", name, granted.Flags())
		}
		if !strings.Contains(child.Meta.SystemPrompt, "no shell execution") {
			t.Errorf("%s was not told it has no shell: %q", name, child.Meta.SystemPrompt)
		}
	}
	reader := family.childWithTask(t, "Read the README")
	if reads := childEventsOf(reader, agent.ToolCallResultEvent); len(reads) != 1 || reads[0].Status != agent.SuccessStatus {
		t.Errorf("%s read %+v, want one successful read", reader.Name, reads)
	}
}

func TestASubagentRotationStartsEachChildOnTheNextModel(t *testing.T) {
	readCall := encodedArguments(map[string]string{"path": "README.md"})
	rig, _ := newRespondingRig(t, delegatingResponder(sim.Call{Name: "read", Arguments: readCall}))
	writeSubagentWorkspace(t, rig)
	writeRigConfig(t, rig, "[subagent]\nround_robin = [\"opencode-go/fake@low\", \"opencode-go/fake@high\"]\n")

	session := rig.start("--yolo", "-m", "opencode-go/fake", delegatingPrompt)
	session.waitFor(followedUpAnswer)
	session.quit()

	family := onlyStoredFamily(t, rig)
	var efforts []string
	for _, name := range family.order {
		child := family.children[name]
		if child.Meta.ModelChoice == nil || child.Meta.ModelChoice.ID != child.Meta.Model {
			t.Errorf("%s stored model %q beside %+v", name, child.Meta.Model, child.Meta.ModelChoice)
		}
		efforts = append(efforts, child.Meta.Effort)
	}
	slices.Sort(efforts)
	if want := []string{"high", "low"}; !slices.Equal(efforts, want) {
		t.Errorf("the children ran at efforts %q, want one at each the rotation names: %q", efforts, want)
	}
}
