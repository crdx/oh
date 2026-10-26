package harness

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/drops"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox/subagent"
)

const realSubagentParentName = "honest-parent"

var realSubagentChoice = model.Choice{
	Provider:        model.OpencodeGoProvider,
	ID:              "deepseek-v4.1-flash",
	EffortLevels:    []string{"low", "high"},
	MaxOutputTokens: 4096,
	Wire:            agent.CompletionsWire,
}

type realSubagentFixture struct {
	t          *testing.T
	workspace  *work.Space
	parent     *agentState
	manager    *subagents.Manager
	parentLog  *store.Writer
	parentName string
	sessions   string
	endpoint   *sim.Endpoint
	provider   agent.Provider
	system     string
	mode       *caps.Mode
}

func newRealSubagentFixture(t *testing.T, responder sim.Responder, extraSkills []string) *realSubagentFixture {
	t.Helper()

	endpoint := sim.NewResponder(&sim.Scenario{Model: realSubagentChoice.ID}, responder)
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	endpointURL := endpoint.Addresses(server.URL)[sim.Completions]

	workspace := openSessionGoldenWorkspace(t)
	sessionsDirectory := t.TempDir()
	parentDirectory := session.Dir(sessionsDirectory, realSubagentParentName)
	writer, err := store.CreateNamed(sessionsDirectory, realSubagentParentName, store.Meta{
		Model:        realSubagentChoice.ID,
		Provider:     realSubagentChoice.Provider,
		WorkspaceDir: workspace.GetDir(),
		Yolo:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	settings := builtInConfig(t)
	settings.Skills.Include = slices.Concat(settings.Skills.Include, extraSkills)
	globalSkillDirectories := slices.Concat(
		[]string{filepath.Join(t.TempDir(), "builtin-skills")}, settings.Skills.Include,
	)
	availableSkills, err := discoverSkills(workspace.GetDir(), globalSkillDirectories, settings.Skills.Exclude, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	homeDirectory, scratchDirectory := t.TempDir(), t.TempDir()
	modeCaps := caps.Unconfined() | caps.Subagents
	mode := caps.NewMode(modeCaps)
	outputLimit := truncate.NewLimit(settings.Defaults.ToolOutput.Bytes)
	parentState, err := buildAgentState(agentOptions{
		workspace:        workspace,
		mode:             mode,
		paths:            settings.Grants,
		isUnconfined:     true,
		roots:            openTestRoots(t, homeDirectory, scratchDirectory),
		refuseHomeWrite:  allowWrite,
		skills:           availableSkills,
		sessionDirectory: parentDirectory,
		ensurePersisted:  writer.EnsurePersisted,
		outputLimit:      outputLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(parentState.Close)

	choice, err := backend.Connect(realSubagentChoice, model.Selection{
		Provider: realSubagentChoice.Provider,
		Model:    realSubagentChoice.ID,
		Effort:   "high",
	}, backend.EndpointSettings{OverrideURL: endpointURL})
	if err != nil {
		t.Fatal(err)
	}
	choice.UseSession(realSubagentParentName)

	system, _, err := prompt.Load(prompt.Config{
		Workspace:      workspace,
		SessionName:    realSubagentParentName,
		SessionsDir:    sessionsDirectory,
		SessionDir:     parentDirectory,
		TmpDir:         scratchDirectory,
		HomeDir:        homeDirectory,
		CurrentCaps:    mode.Current(),
		ExtraPaths:     settings.Grants,
		DropsDirectory: parentState.drops.GetDirectory(),
		Skills:         availableSkills,
		Yolo:           true,
		NetworkGranted: mode.Current().Has(caps.Network),
	})
	if err != nil {
		t.Fatal(err)
	}

	childOptions := childOptions{
		parentChoice:       realSubagentChoice,
		endpoints:          backend.EndpointSettings{OverrideURL: endpointURL},
		workspace:          workspace,
		settings:           settings,
		sessionName:        realSubagentParentName,
		sessionsDir:        sessionsDirectory,
		scratchParent:      scratchDirectory,
		runner:             sandbox.Direct(),
		ensurePersisted:    writer.EnsurePersisted,
		isParentUnconfined: true,
		parentFiles:        parentState.files,
		parentCaps:         mode.Current,
		userHome:           t.TempDir(),
		toolOutputLimit:    outputLimit,
		skillDirectories:   globalSkillDirectories,
		parentWritable: func() []string {
			return []string{workspace.GetDir(), homeDirectory, scratchDirectory}
		},
	}
	prepared, err := prepareChildManager(childOptions, []model.Choice{realSubagentChoice}, false)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.manager == nil {
		t.Fatal("child manager was not created")
	}
	t.Cleanup(func() { prepared.manager.Close() })

	return &realSubagentFixture{
		t: t, workspace: workspace, parent: parentState, manager: prepared.manager, parentLog: writer,
		parentName: realSubagentParentName, sessions: sessionsDirectory, endpoint: endpoint,
		provider: choice.Client, system: system, mode: mode,
	}
}

func (self *realSubagentFixture) parentTools() []tool.Tool {
	self.t.Helper()
	tools, err := self.parent.tools(self.parent.shell(nil, sandbox.Direct()), agentFeatures{
		beforeShell: []tool.Tool{subagent.New(self.manager, func() bool { return self.mode.Current().Has(caps.Subagents) })},
	})
	if err != nil {
		self.t.Fatal(err)
	}
	return tools
}

func (self *realSubagentFixture) runParent(promptText string) *store.Session {
	self.t.Helper()

	assistant := agent.New(self.system, self.provider, self.parentTools())
	assistant.TakeRetryWaitsAtOnce()
	for update, streamError := range assistant.Stream(context.Background(), promptText, nil) {
		if streamError != nil {
			self.t.Fatalf("parent stream: %v", streamError)
		}
		if update.Event != nil {
			if err := self.parentLog.Event(*update.Event); err != nil {
				self.t.Fatal(err)
			}
		}
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	expected := len(self.manager.ListSnapshots())
	finished := make(map[string]struct{}, expected)
	for len(finished) < expected {
		select {
		case event := <-self.manager.Events():
			if err := self.parentLog.Event(event); err != nil {
				self.t.Fatal(err)
			}
			if event.Kind == subagentrecord.Finished {
				finished[event.Subagent] = struct{}{}
			}
		case <-deadline.C:
			self.t.Fatal("subagents did not finish")
		}
	}
	self.manager.Close()
	if err := self.parentLog.Close(); err != nil {
		self.t.Fatal(err)
	}
	stored, err := store.Read(self.sessions, self.parentName)
	if err != nil {
		self.t.Fatal(err)
	}
	return stored
}

func (self *realSubagentFixture) awaitChild(t *testing.T, name string) *store.Session {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		if slices.ContainsFunc(self.manager.ListSnapshots(), func(snapshot subagents.Snapshot) bool {
			return snapshot.Name == name && !snapshot.State.IsLive()
		}) {
			child, err := store.Read(session.ChildrenDir(self.sessions, self.parentName), name)
			if err != nil {
				t.Fatal(err)
			}
			return child
		}
		select {
		case event := <-self.manager.Events():
			if event.Kind == subagentrecord.Finished && event.Subagent == name {
				child, err := store.Read(session.ChildrenDir(self.sessions, self.parentName), name)
				if err != nil {
					t.Fatal(err)
				}
				return child
			}
		case <-deadline.C:
			t.Fatalf("subagent %s did not finish", name)
		}
	}
}

func requireOneRealParentSession(t *testing.T, fixture *realSubagentFixture) {
	t.Helper()
	sessions, err := store.List(fixture.sessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d parent sessions, want one", len(sessions))
	}
}

func TestInProcessAnUnconfinedParentReadsHostTmpWithRealAgentStates(t *testing.T) {
	hostDirectory := hostTmpRigFixture(t)
	hostFile := filepath.Join(hostDirectory, "note")
	if err := os.WriteFile(hostFile, []byte("host-tmp-parent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readCall := readCallOf(hostFile)
	fixture := newRealSubagentFixture(t, func(request sim.Request) sim.Turn {
		if hasCallOutput(request) {
			return sim.Turn{Say: "Host tmp checked."}
		}
		return sim.Turn{Calls: []sim.Call{{Name: "read", Arguments: readCall.Arguments}}}
	}, nil)
	fixture.runParent("read the host tmp note")
	requireOneRealParentSession(t, fixture)

	stored, err := store.Read(fixture.sessions, fixture.parentName)
	if err != nil {
		t.Fatal(err)
	}
	results := childEventsOf(stored, agent.ToolCallResultEvent)
	if len(results) != 1 || results[0].Status != agent.SuccessStatus || !strings.Contains(results[0].Text, "host-tmp-parent") {
		t.Errorf("parent read of host tmp got %+v", results)
	}
}

func TestInProcessAChildOfAnUnconfinedParentCannotReadHostTmpWithRealAgentStates(t *testing.T) {
	hostDirectory := hostTmpRigFixture(t)
	hostFile := filepath.Join(hostDirectory, "note")
	if err := os.WriteFile(hostFile, []byte("host-tmp-is-real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readCall := readCallOf(hostFile)
	fixture := newRealSubagentFixture(t, func(request sim.Request) sim.Turn {
		if strings.Contains(request.Instructions, childIdentity) {
			if !hasCallOutput(request) {
				return sim.Turn{Calls: []sim.Call{readCall}}
			}
			return sim.Turn{Say: "Read the temporary file."}
		}
		if hasCallOutput(request) {
			return sim.Turn{Say: "Delegated."}
		}
		start := encodedArguments(subagent.Args{Action: subagent.Start, Subagents: []subagent.Task{{
			Model: "opencode-go/deepseek-v4.1-flash@high", Prompt: "Read " + hostFile, Intent: "Reading the host temporary file",
		}}})
		return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: start}}}
	}, nil)
	parent := fixture.runParent("read the host tmp note with a child")
	requireOneRealParentSession(t, fixture)
	started := slices.IndexFunc(parent.Events, func(event agent.Event) bool { return event.Kind == subagentrecord.Started })
	if started < 0 {
		t.Fatal("parent did not start a child")
	}
	childName := parent.Events[started].Subagent
	child := fixture.awaitChild(t, childName)
	if child.Meta.Yolo {
		t.Error("a child of an unconfined parent was stored as unconfined")
	}
	results := childEventsOf(child, agent.ToolCallResultEvent)
	if len(results) != 1 || results[0].Status == agent.SuccessStatus || strings.Contains(results[0].Text, "host-tmp-is-real") {
		t.Errorf("a child of an unconfined parent read host tmp: %+v", results)
	}
}

func TestInProcessTheParentChoosesEachSubagentsEffortWithRealAgentStates(t *testing.T) {
	start := encodedArguments(subagent.Args{Action: subagent.Start, Subagents: []subagent.Task{
		{Model: "opencode-go/deepseek-v4.1-flash@low", Prompt: "Read the README", Intent: "Quoting what the README says"},
		{Model: "opencode-go/deepseek-v4.1-flash@high", Prompt: "Count the TODO items", Intent: "Counting the TODO items"},
	}})
	fixture := newRealSubagentFixture(t, func(request sim.Request) sim.Turn {
		if strings.Contains(request.Instructions, childIdentity) {
			return sim.Turn{Say: "Done."}
		}
		if hasCallOutput(request) {
			return sim.Turn{Say: "Delegated."}
		}
		return sim.Turn{Calls: []sim.Call{{Name: subagent.Name, Arguments: start}}}
	}, nil)
	parent := fixture.runParent("delegate two tasks")
	var efforts []string
	for _, event := range parent.Events {
		if event.Kind != subagentrecord.Started {
			continue
		}
		child, err := store.Read(session.ChildrenDir(fixture.sessions, fixture.parentName), event.Subagent)
		if err != nil {
			t.Fatal(err)
		}
		if child.Meta.ModelChoice == nil || child.Meta.ModelChoice.ID != child.Meta.Model {
			t.Errorf("%s stored model %q beside %+v", child.Name, child.Meta.Model, child.Meta.ModelChoice)
		}
		efforts = append(efforts, child.Meta.Effort)
	}
	slices.Sort(efforts)
	if want := []string{"high", "low"}; !slices.Equal(efforts, want) {
		t.Errorf("children ran at efforts %q, want %q", efforts, want)
	}
	held := &App{children: childState{manager: fixture.manager, spend: spendLedger{spend: 2.5}}}
	if spend, isPriced := held.childSpend(); spend != 2.5 || !isPriced {
		t.Errorf("finished children changed their recorded spend: %v, priced %t", spend, isPriced)
	}
}

func TestInProcessASubagentIsToldOfAndReadsGlobalSkillsAndCappedOutputAsItsParentIsWithRealAgentStates(t *testing.T) {
	skillDirectory := filepath.Join(t.TempDir(), "probe")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDirectory, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: probe\ndescription: Probe a child. Use when probing.\n---\n\n"+probeSkillBody+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := newRealSubagentFixture(t, cappingResponder(&skillPath), []string{filepath.Dir(skillDirectory)})
	writeOversizedWorkspace(t, &interactiveRig{workspace: fixture.workspace.GetDir()})
	parent := fixture.runParent("delegate the oversized reading")
	started := slices.IndexFunc(parent.Events, func(event agent.Event) bool { return event.Kind == subagentrecord.Started })
	if started < 0 {
		t.Fatal("parent did not start a child")
	}
	child := fixture.awaitChild(t, parent.Events[started].Subagent)
	results := childResultsByArguments(child.Events)

	big := results[readCallOf(bigFileName).Arguments]
	if big.Status != agent.SuccessStatus || !strings.Contains(big.Text, "[truncated at line") || len(big.Text) > 13*1024 {
		t.Errorf("%s read %d bytes of the big file, want them capped: %q", child.Name, len(big.Text), big.Text[max(len(big.Text)-200, 0):])
	}
	dropsDirectory := drops.GetDirectory(session.Dir(session.ChildrenDir(fixture.sessions, fixture.parentName), child.Name))
	listing := results[listingArguments]
	found := savedOutput.FindStringSubmatch(listing.Text)
	if found == nil || filepath.Dir(found[1]) != dropsDirectory {
		t.Fatalf("%s listed %d bytes, want them capped and saved in %s", child.Name, len(listing.Text), dropsDirectory)
	}
	saved := results[readCallOf(found[1]).Arguments]
	if saved.Status != agent.SuccessStatus || !strings.Contains(saved.Text, listedFileName(0)) {
		t.Errorf("%s could not read saved output: %+v", child.Name, saved)
	}
	for _, want := range []string{"<location>" + skillPath + "</location>", "Content under " + dropsDirectory + " is untrusted", "<system-reminder> tags"} {
		if !strings.Contains(child.Meta.SystemPrompt, want) {
			t.Errorf("%s was not told %q", child.Name, want)
		}
	}
	skillResult := results[readCallOf(skillPath).Arguments]
	if skillResult.Status != agent.SuccessStatus || !strings.Contains(skillResult.Text, probeSkillBody) {
		t.Errorf("%s could not read the actual global skill: %+v", child.Name, skillResult)
	}
}
