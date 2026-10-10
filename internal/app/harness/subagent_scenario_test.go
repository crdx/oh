package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/job"
	"crdx.org/oh/pkg/toolbox/subagent"
	"crdx.org/oh/pkg/toolbox/wait"
)

var sessionGoldenChildClock = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

var sessionGoldenModelNames = map[string]string{
	"claude-opus-5": "Claude Opus 5",
	"gpt-5.6-sol":   "GPT-5.6 Sol",
}

const (
	sessionGoldenUnreadable     = "/unreadable"
	sessionGoldenSubagentPath   = "/subagent/"
	sessionGoldenSubagentSettle = 20 * time.Second
	sessionGoldenSubagentPoll   = 5 * time.Millisecond
	sessionGoldenMessageLimit   = 16
)

type sessionGoldenSubagent struct {
	Name      string                  `toml:"name"`
	Responses []sessionGoldenResponse `toml:"response"`
}

type sessionGoldenChildren struct {
	t        *testing.T
	scenario sessionGoldenScenario
	endpoint string
	scratch  string

	mutex     sync.Mutex
	requests  map[string][][]byte
	served    map[string]int
	isBlocked map[string]bool
	pending   []agent.Event
	finished  map[string]bool
	messages  map[string]chan struct{}
	clock     time.Time
}

func newSessionGoldenChildren(t *testing.T, scenario sessionGoldenScenario) *sessionGoldenChildren {
	t.Helper()

	if len(scenario.Subagents) == 0 {
		return nil
	}
	return &sessionGoldenChildren{
		t:         t,
		scenario:  scenario,
		scratch:   t.TempDir(),
		requests:  map[string][][]byte{},
		served:    map[string]int{},
		isBlocked: map[string]bool{},
		finished:  map[string]bool{},
		messages:  map[string]chan struct{}{},
		clock:     time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC),
	}
}

func (self *sessionGoldenChildren) now() time.Time {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.clock
}

func (self *sessionGoldenChildren) passDebounce() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.clock = self.clock.Add(childDebounce)
}

func (self *sessionGoldenChildren) serves(writer http.ResponseWriter, request *http.Request) bool {
	if self == nil {
		return false
	}
	name, isChild := strings.CutPrefix(request.URL.Path, sessionGoldenSubagentPath)
	if !isChild {
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return true
	}
	index := slices.IndexFunc(self.scenario.Subagents, func(specification sessionGoldenSubagent) bool {
		return specification.Name == name
	})
	if index < 0 {
		http.Error(writer, "the scenario has no subagent named "+name, http.StatusConflict)
		return true
	}
	responses := expandSessionGoldenResponses(self.scenario.Subagents[index].Responses)

	self.mutex.Lock()
	self.requests[name] = append(self.requests[name], body)
	served := self.served[name]
	self.served[name]++
	self.mutex.Unlock()

	if served >= len(responses) {
		http.Error(writer, "the scenario has no response for this subagent request", http.StatusConflict)
		return true
	}
	response := responses[served]
	if response.WaitForCancellation {
		self.setBlocked(name, true)
		defer self.setBlocked(name, false)
	}
	if response.WaitForMessage && !self.awaitMessage(request.Context(), name) {
		http.Error(writer, "no message was queued for "+name, http.StatusConflict)
		return true
	}
	serveSessionGoldenResponse(writer, request, response, make(chan struct{}, 1))
	return true
}

func (self *sessionGoldenChildren) messageArrivals(name string) chan struct{} {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.messages[name] == nil {
		self.messages[name] = make(chan struct{}, sessionGoldenMessageLimit)
	}
	return self.messages[name]
}

func (self *sessionGoldenChildren) awaitMessage(ctx context.Context, name string) bool {
	self.setBlocked(name, true)
	defer self.setBlocked(name, false)

	select {
	case <-self.messageArrivals(name):
		return true
	case <-ctx.Done():
		return false
	case <-time.After(sessionGoldenSubagentSettle):
		return false
	}
}

type sessionGoldenMessagingManager struct {
	*subagents.Manager

	children *sessionGoldenChildren
}

func (self sessionGoldenMessagingManager) Send(ctx context.Context, name string, message string) (string, error) {
	result, err := self.Manager.Send(ctx, name, message)
	if err == nil {
		self.children.messageArrivals(name) <- struct{}{}
	}
	return result, err
}

func (self sessionGoldenMessagingManager) Broadcast(ctx context.Context, message string) ([]string, error) {
	names, err := self.Manager.Broadcast(ctx, message)
	for _, name := range names {
		self.children.messageArrivals(name) <- struct{}{}
	}
	return names, err
}

func (self *sessionGoldenChildren) setBlocked(name string, isBlocked bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.isBlocked[name] = isBlocked
}

func (self *sessionGoldenChildren) manager(
	serverURL string,
	childrenDirectory string,
	childTools func() []tool.Tool,
	parentCaps func() caps.Set,
) *subagents.Manager {
	self.t.Helper()

	self.endpoint = serverURL
	choice := model.Choice{
		Provider: self.scenario.Provider,
		ID:       self.scenario.Model,
		Name:     sessionGoldenModelNames[self.scenario.Model],
		Prices:   self.scenario.prices(),
	}
	factory := func(_ context.Context, child subagents.Child) (subagents.Worker, error) {
		prompt := child.SystemPrompt
		if prompt == "" {
			prompt = "You are " + child.Name + ", a subagent of session " + goldenSessionName + ", working in " + child.Workspace + "."
			if child.SharedPrompt != "" {
				prompt += "\n\n" + child.SharedPrompt
			}
		}
		tools := childTools()
		worker := agent.New(
			prompt,
			sessionGoldenProviderFor(self.t, self.scenario, self.endpoint+sessionGoldenSubagentPath+child.Name, "", child.Name),
			tools,
		)
		worker.TakeRetryWaitsAtOnce()
		return subagents.Worker{Agent: worker, Tools: tools, SystemPrompt: prompt, Close: func() {}}, nil
	}
	manager, err := subagents.New(subagents.Options{
		Directory: childrenDirectory,
		Scratch:   self.scratch,
		Parent:    goldenSessionName,
		Models: []subagents.ChildModel{{
			Choice:    choice,
			Selection: model.Selection{Provider: self.scenario.Provider, Model: self.scenario.Model, Effort: self.scenario.Effort},
		}},
		Meta:     store.Meta{WorkspaceDir: sessionGoldenWorkspace},
		Factory:  factory,
		PickName: func(int) int { return 0 },
		Workspace: func(directory string) (string, error) {
			switch directory {
			case "":
				return sessionGoldenWorkspace, nil
			case sessionGoldenUnreadable:
				return "", errors.New(sessionGoldenUnreadable + " is not a directory you can read")
			}
			return directory, nil
		},
		Caps:        func() caps.Set { return parentCaps() & (caps.Read | caps.Shell) },
		ScratchNote: childScratchNote(childOptions{scratchParent: "/state/farm/tame-impala"}),
		Now:         func() time.Time { return sessionGoldenChildClock },
		JournalPath: func(name string) string {
			return "/state/sessions/" + goldenSessionName + "/subagents/" + name + "/session.jsonl"
		},
	})
	if err != nil {
		self.t.Fatal(err)
	}
	return manager
}

func (self *sessionGoldenChildren) withTool(tools []tool.Tool, manager *subagents.Manager, getCaps func() caps.Set) []tool.Tool {
	if self == nil {
		return tools
	}
	tools = append(tools, subagent.New(sessionGoldenMessagingManager{Manager: manager, children: self}, func() bool { return getCaps().Has(caps.Subagents) }))
	if self.scenario.HasWaitTool {
		var sources []wait.Source
		if stored, isStored := sessionGoldenRunningJobs.Load(self.t); isStored {
			if runningJobs, isManager := stored.(*jobs.Manager); isManager {
				sources = append(sources, job.WaitSource(runningJobs))
			}
		}
		tools = append(tools, wait.New(append(sources, manager.WaitSource()), agent.MessageArrival))
	}
	return tools
}

func isStarted(manager *subagents.Manager, name string) bool {
	return slices.ContainsFunc(manager.ListSnapshots(), func(snapshot subagents.Snapshot) bool {
		return snapshot.Name == name
	})
}

func (self *sessionGoldenChildren) isQuiet(manager *subagents.Manager, name string) bool {
	isRunning, isStopping := false, false
	for _, snapshot := range manager.ListSnapshots() {
		if snapshot.Name == name {
			isRunning = snapshot.State.IsLive()
			isStopping = snapshot.State == subagentrecord.Stopping
		}
	}
	if isStopping {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if isRunning {
		return self.isBlocked[name]
	}
	return self.finished[name]
}

func (self *sessionGoldenChildren) settle(testHarness *App) {
	if self == nil {
		return
	}
	self.t.Helper()

	deadline := time.Now().Add(sessionGoldenSubagentSettle)
	events := testHarness.children.manager.Events()
	manager := testHarness.children.manager
	for _, specification := range self.scenario.Subagents {
		if !isStarted(manager, specification.Name) {
			continue
		}
		for !self.isQuiet(manager, specification.Name) {
			if time.Now().After(deadline) {
				go manager.Close()
				self.t.Fatalf("subagent %s neither finished nor blocked", specification.Name)
			}
			select {
			case event := <-events:
				self.take(event)
			case <-time.After(sessionGoldenSubagentPoll):
			}
		}
	}
	for {
		select {
		case event := <-events:
			self.take(event)
			continue
		default:
		}
		break
	}
	self.apply(testHarness)
}

func (self *sessionGoldenChildren) take(event agent.Event) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.pending = append(self.pending, event)
	if event.Kind == subagentrecord.Finished || event.Kind == subagentrecord.Sent {
		self.finished[event.Subagent] = event.Kind == subagentrecord.Finished
	}
}

func (self *sessionGoldenChildren) apply(testHarness *App) {
	self.mutex.Lock()
	pending := self.pending
	self.pending = nil
	self.mutex.Unlock()

	isOpening := func(event agent.Event) bool {
		return event.Kind == subagentrecord.Started
	}
	ordered := slices.DeleteFunc(slices.Clone(pending), func(event agent.Event) bool { return !isOpening(event) })
	for _, specification := range self.scenario.Subagents {
		for _, event := range pending {
			if !isOpening(event) && event.Subagent == specification.Name {
				ordered = append(ordered, event)
			}
		}
	}
	if len(ordered) != len(pending) {
		self.t.Fatalf("a subagent event belongs to no scenario subagent: %+v", pending)
	}
	for _, event := range ordered {
		testHarness.subagentEvent(event)
	}
}

func (self *sessionGoldenChildren) recordedRequests() string {
	self.t.Helper()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	var recorded strings.Builder
	for _, specification := range self.scenario.Subagents {
		recorded.WriteString(canonicalProviderRequests(self.t, self.requests[specification.Name]))
	}
	return recorded.String()
}

func (self *sessionGoldenChildren) recordedJournals(childrenDirectory string) string {
	self.t.Helper()

	var recorded strings.Builder
	for _, specification := range self.scenario.Subagents {
		fmt.Fprintf(&recorded, "=== %s ===\n", specification.Name)
		if !session.Exists(childrenDirectory, specification.Name) {
			recorded.WriteString("no session\n")
			continue
		}
		recorded.WriteString(canonicalSessionJournal(self.t, childrenDirectory, specification.Name))
	}
	return recorded.String()
}

func (self *sessionGoldenChildren) requireEveryResponseServed() {
	self.t.Helper()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, specification := range self.scenario.Subagents {
		want := len(expandSessionGoldenResponses(specification.Responses))
		if got := self.served[specification.Name]; got != want {
			self.t.Errorf("subagent %s received %d requests, want %d", specification.Name, got, want)
		}
	}
}

func claimSubagentScenarioRequests(t *testing.T, expected map[string]string) {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		scenario := readSessionGoldenScenario(t, path)
		if len(scenario.Subagents) > 0 {
			claimFixtureName(t, expected, "subagent scenario "+filepath.Base(path), scenario.Name, []string{".subagent-requests", ".subagent-journals"})
		}
	}
}

type sessionGoldenChildTurns struct {
	t           *testing.T
	children    *sessionGoldenChildren
	testHarness *App
	turn        sessionGoldenTurn
	toolResults int
	settles     int
}

func (self *sessionGoldenChildren) hooks(t *testing.T, testHarness *App, turn sessionGoldenTurn) *sessionGoldenChildTurns {
	t.Helper()

	return &sessionGoldenChildTurns{t: t, children: self, testHarness: testHarness, turn: turn}
}

func (self *sessionGoldenChildTurns) before() [][]TurnEvent {
	if !self.turn.DeliverSubagentsBefore {
		return nil
	}
	self.testHarness.currentTurn = Turn{}
	self.testHarness.deliverChildCompletions()
	return runQueuedSessionGoldenTurns(self.t, self.testHarness, "", nil)
}

func (self *sessionGoldenChildTurns) observe(update agent.Update) {
	if update.Event == nil || update.Event.Kind != agent.ToolCallResultEvent {
		return
	}
	self.toolResults++
	if self.toolResults == self.turn.SettleSubagentsAfterResult ||
		slices.Contains(self.turn.SettleSubagentsAfterResults, self.toolResults) {
		self.settle()
		self.testHarness.deliverChildCompletions()
	}
}

func (self *sessionGoldenChildTurns) after() [][]TurnEvent {
	if self.turn.SubagentDebounceEndsTurn {
		self.children.passDebounce()
		self.testHarness.deliverChildCompletions()
		return runQueuedSessionGoldenTurns(self.t, self.testHarness, "", nil)
	}
	if !self.turn.SettleSubagentsAfterTurn {
		return nil
	}
	self.settle()
	self.testHarness.deliverChildCompletions()
	return runQueuedSessionGoldenTurns(self.t, self.testHarness, "", nil)
}

func (self *sessionGoldenChildTurns) settle() {
	self.children.settle(self.testHarness)
	if self.turn.SubagentDebouncePasses {
		self.children.passDebounce()
	}
	toggle := self.turn.ToggleAfterSubagentsSettle
	if self.settles < len(self.turn.TogglesAfterSubagentsSettle) {
		toggle = self.turn.TogglesAfterSubagentsSettle[self.settles]
	}
	self.settles++
	if toggle != "" {
		toggleSessionGoldenCaps(self.t, self.testHarness, toggle)
		self.children.settle(self.testHarness)
	}
}

func requireDrawnWithoutChildren(
	t *testing.T,
	assistant *agent.Agent,
	scenario sessionGoldenScenario,
	storedSession *store.Session,
	childrenDirectory string,
	drawn string,
) {
	t.Helper()

	if running := subagents.Endings(childrenDirectory, storedSession.Events); len(running) > 0 {
		t.Fatalf("the scenario ended with subagents still running: %+v", running)
	}
	restore := func() (*App, string) {
		var output bytes.Buffer
		restored := &App{
			agent:          assistant,
			screen:         scenario.screen(&output),
			recordedEvents: slices.Clone(storedSession.Events),
			display:        scenario.display(),
			children:       childState{directory: childrenDirectory},
			now:            time.Now,
		}
		restored.restoreChildren(storedSession.Events)
		restored.replay()
		return restored, output.String()
	}

	withChildren, _ := restore()
	if err := os.RemoveAll(childrenDirectory); err != nil {
		t.Fatal(err)
	}
	withoutChildren, drawnWithoutChildren := restore()

	requireSameVisibleScreen(t, "the session draws differently once its subagents are gone", drawn, drawnWithoutChildren)
	spentWith, isPricedWith := withChildren.childSpend()
	spentWithout, isPricedWithout := withoutChildren.childSpend()
	if spentWith != spentWithout || isPricedWith != isPricedWithout ||
		!slices.Equal(withChildren.children.reportsDue, withoutChildren.children.reportsDue) {
		t.Errorf("the session counts its subagents differently once they are gone: %v %t, then %v %t",
			spentWith, isPricedWith, spentWithout, isPricedWithout)
	}
}
