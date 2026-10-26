package harness

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
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
	"crdx.org/oh/pkg/toolbox/messageparent"
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

func hostTmpRigFixture(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(sandbox.TmpDir, "oh-yolo-host-") //nolint:usetesting // the yolo test must reach host /tmp
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

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
	childScratch = regexp.MustCompile(`at (\S+) on the host`)
	readmeReader = regexp.MustCompile(`(\S+) done(?: \([^)]*\))?: ` + regexp.QuoteMeta(firstChildAnswer))
)

func newRespondingRig(t *testing.T, respond sim.Responder) (*interactiveRig, *sim.Endpoint) {
	t.Helper()

	endpoint := sim.NewResponder(&sim.Scenario{Model: "deepseek-v4.1-flash"}, respond)
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
		{Model: "opencode-go/deepseek-v4.1-flash@low", Prompt: "Read the README and quote it", Intent: "Quoting what the README says"},
		{Model: "opencode-go/deepseek-v4.1-flash@high", Prompt: "Count the TODO items", Intent: "Counting the TODO items in docs", Workspace: secondWorkspace},
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
	scratch := found[1]
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

	session := rig.start("--yolo", "-c", "s", "-m", "opencode-go/deepseek-v4.1-flash", delegatingPrompt)
	session.waitFor(delegatedAnswer)
	session.waitFor(firstChildAnswer)
	session.waitFor(followedUpAnswer)
	session.requireShown(secondChildAnswer)

	session.typeText("/subs" + pressEnter)
	session.waitFor("Finished subagents:")
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

func TestANewSessionOffersSubagentsWithoutAConfiguredRotation(t *testing.T) {
	for _, test := range []struct {
		name      string
		config    string
		arguments []string
		want      caps.Set
	}{
		{
			name:   "defaulted on with no subagent model",
			config: "[default]\ncaps = \"rs\"\n",
			want:   caps.Unconfined() | caps.Subagents,
		},
		{
			name:   "defaulted on with a concurrency limit",
			config: "[default]\ncaps = \"rs\"\n\n[subagent]\nconcurrency = 2\n",
			want:   caps.Unconfined() | caps.Subagents,
		},
		{
			name:      "chosen with no subagent model",
			arguments: []string{"-c", "s"},
			want:      caps.Unconfined() | caps.Subagents,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rig, _ := newRespondingRig(t, func(sim.Request) sim.Turn { return sim.Turn{Say: "Noted."} })
			writeRigConfig(t, rig, test.config)
			arguments := append([]string{"-p", "--yolo", "-m", "opencode-go/deepseek-v4.1-flash"}, test.arguments...)
			runTestBinary(t, rig.binary, rig.workspace, rig.environment, append(arguments, "hello")...)

			storedSessions := rig.storedSessions()
			if len(storedSessions) != 1 {
				t.Fatalf("got %d stored sessions, want one", len(storedSessions))
			}
			if got, found := caps.LastRecordedMode(storedSessions[0].Events); !found || got != test.want {
				t.Errorf("opened in %s (recorded: %t), want %s", got.Flags(), found, test.want.Flags())
			}
		})
	}
}

const (
	messagingPrompt     = "have a subagent check the README"
	childQuestion       = "The README is short. Should I quote it?"
	parentReply         = "Yes, quote it"
	childQuotedAnswer   = "It says hello."
	parentStartedAnswer = "It is checking."
	parentRepliedAnswer = "Replied to it."
	parentFinalAnswer   = "It quoted the README."
)

var interjector = regexp.MustCompile(`Subagent (\S+) interjects:`)

func messagingResponder() sim.Responder {
	start := encodedArguments(subagent.Args{Action: subagent.Start, Subagents: []subagent.Task{
		{Model: "opencode-go/deepseek-v4.1-flash@low", Prompt: "Check the README", Intent: "Checking what the README says"},
	}})
	ask := encodedArguments(map[string]string{"message": childQuestion})
	read := encodedArguments(map[string]string{"path": "README.md"})
	replied := make(chan struct{})
	var once sync.Once
	return func(request sim.Request) sim.Turn {
		if strings.Contains(request.Instructions, childIdentity) {
			switch {
			case mentions(request, parentReply):
				return sim.Turn{Say: childQuotedAnswer}
			case hasCallOutput(request):
				<-replied
				return sim.Turn{Calls: []sim.Call{{Name: "read", Arguments: read}}}
			}
			return sim.Turn{Calls: []sim.Call{{Name: messageparent.Name, Arguments: ask}}}
		}
		switch {
		case mentions(request, childQuotedAnswer):
			return sim.Turn{Say: parentFinalAnswer}
		case mentions(request, "queued for "):
			once.Do(func() { close(replied) })
			return sim.Turn{Say: parentRepliedAnswer}
		case mentions(request, childQuestion):
			var name string
			for _, entry := range request.Input {
				if found := interjector.FindStringSubmatch(entry.Content); found != nil {
					name = found[1]
				}
			}
			send := encodedArguments(subagent.Args{Action: subagent.Send, Name: name, Message: parentReply})
			return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: send}}}
		case hasCallOutput(request):
			return sim.Turn{Say: parentStartedAnswer}
		}
		return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: start}}}
	}
}
