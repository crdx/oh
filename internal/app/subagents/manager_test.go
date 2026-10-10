package subagents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/subagent"
)

const parentName = "frugal-ladybird"

type answeringProvider struct {
	prompts []string
}

func (self *answeringProvider) Configure(string, []tool.Definition) {}
func (self *answeringProvider) AddUserMessage(message string) {
	self.prompts = append(self.prompts, message)
}
func (self *answeringProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *answeringProvider) Dump() []json.RawMessage {
	items := make([]json.RawMessage, len(self.prompts))
	for index, prompt := range self.prompts {
		items[index] = json.RawMessage(strconv.Quote(prompt))
	}
	return items
}

func (self *answeringProvider) Load(items []json.RawMessage) {
	self.prompts = nil
	for _, item := range items {
		var prompt string
		if json.Unmarshal(item, &prompt) == nil {
			self.prompts = append(self.prompts, prompt)
		}
	}
}

func (self *answeringProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	if err := ctx.Err(); err != nil {
		return agent.Reply{}, err
	}
	answer := strings.Join(self.prompts, " then ")
	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: answer, Done: false})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true, Usage: &agent.Usage{
		InputTokens: 100, OutputTokens: 10, Cache: &agent.CacheUsage{},
	}})
	return agent.Reply{}, nil
}

type toolUsingProvider struct {
	wasAnswered bool
	result      string
}

func (self *toolUsingProvider) Configure(string, []tool.Definition) {}
func (self *toolUsingProvider) AddUserMessage(string)               {}
func (self *toolUsingProvider) AddToolResults(results []agent.ToolCallResult) {
	self.result = results[0].Output
}

func (self *toolUsingProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	if !self.wasAnswered {
		self.wasAnswered = true
		yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: "I will use a tool."})
		yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true})
		return agent.Reply{Calls: []agent.ToolCall{{ID: "probe-1", Name: "probe", Arguments: `{}`}}}, nil
	}
	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: self.result})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})
	return agent.Reply{}, nil
}

type waitingProvider struct{}

func (waitingProvider) Configure(string, []tool.Definition)   {}
func (waitingProvider) AddUserMessage(string)                 {}
func (waitingProvider) AddToolResults([]agent.ToolCallResult) {}
func (waitingProvider) Send(ctx context.Context, _ agent.Yield) (agent.Reply, error) {
	<-ctx.Done()
	return agent.Reply{}, ctx.Err()
}

type testFamily struct {
	directory string
	scratch   string
	workspace string
	caps      *caps.Set
}

func newTestFamily(t *testing.T) testFamily {
	t.Helper()
	root := t.TempDir()
	family := testFamily{
		directory: session.ChildrenDir(filepath.Join(root, "sessions"), parentName),
		scratch:   filepath.Join(root, "farm", parentName),
		workspace: filepath.Join(root, "workspace"),
		caps:      new(caps.Set),
	}
	*family.caps = caps.Read | caps.Shell
	for _, directory := range []string{family.scratch, family.workspace} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return family
}

func (self testFamily) manager(t *testing.T, factory Factory) *Manager {
	t.Helper()
	choice := model.Choice{Provider: "anthropic", ID: "claude-opus-5"}
	manager, err := New(Options{
		Directory: self.directory,
		Scratch:   self.scratch,
		Parent:    parentName,
		Choice:    choice,
		Meta:      store.Meta{Provider: "anthropic", Model: "claude-opus-5", ModelChoice: &choice},
		Factory:   factory,
		PickName:  func(int) int { return 0 },
		Workspace: func(directory string) (string, error) {
			switch directory {
			case "":
				return self.workspace, nil
			case "/unreadable":
				return "", errors.New("/unreadable is not a directory you can read")
			}
			return directory, nil
		},
		Caps: func() caps.Set { return *self.caps },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func answering(provider func() agent.Provider) Factory {
	return func(_ context.Context, child Child) (Worker, error) {
		prompt := child.SystemPrompt
		if prompt == "" {
			prompt = "child of " + parentName + " in " + child.Workspace
		}
		return Worker{Agent: agent.New(prompt, provider(), nil), SystemPrompt: prompt, Close: func() {}}, nil
	}
}

func waiting() Factory {
	return answering(func() agent.Provider { return waitingProvider{} })
}

func untilSettled(t *testing.T, manager *Manager, name string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for slices.ContainsFunc(manager.ListSnapshots(), func(snapshot Snapshot) bool {
		return snapshot.Name == name && snapshot.State.IsLive()
	}) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never settled", name)
		}
		time.Sleep(time.Millisecond)
	}
}

func untilFinished(t *testing.T, manager *Manager, count int) []agent.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var events []agent.Event
	for finished := 0; finished < count; {
		select {
		case event := <-manager.Events():
			events = append(events, event)
			if event.Kind == subagentrecord.Finished {
				finished++
			}
		case <-ctx.Done():
			t.Fatalf("only %d of %d children finished", finished, count)
		}
	}
	return events
}

func TestAtMostFiveChildrenRunAtOnceAcrossCalls(t *testing.T) {
	manager := newTestFamily(t).manager(t, waiting())
	tasks := func(count int) []subagent.Task {
		return slices.Repeat([]subagent.Task{{Prompt: "wait"}}, count)
	}
	if _, err := manager.Start(t.Context(), "", tasks(6)); err == nil || !strings.Contains(err.Error(), "at most 5") {
		t.Fatalf("six children in one call were started: %v", err)
	}
	if _, err := manager.Start(t.Context(), "", tasks(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(t.Context(), "", tasks(2)); err != nil {
		t.Fatalf("a second batch within the limit was refused: %v", err)
	}
	if _, err := manager.Start(t.Context(), "", tasks(1)); err == nil || !strings.Contains(err.Error(), "at most 5") {
		t.Fatalf("a sixth running child was started: %v", err)
	}
	if got := len(manager.ListSnapshots()); got != 5 {
		t.Errorf("got %d children, want 5", got)
	}
}

func TestChildrenShareTheParentsAdjectiveAndNotItsEmoji(t *testing.T) {
	manager := newTestFamily(t).manager(t, waiting())
	if _, err := manager.Start(t.Context(), "", slices.Repeat([]subagent.Task{{Prompt: "wait"}}, 3)); err != nil {
		t.Fatal(err)
	}
	emojis := []string{session.Emoji(parentName)}
	for _, snapshot := range manager.ListSnapshots() {
		if !strings.HasPrefix(snapshot.Name, "frugal-") {
			t.Errorf("%s does not share its parent's adjective", snapshot.Name)
		}
		if slices.Contains(emojis, session.Emoji(snapshot.Name)) {
			t.Errorf("%s repeats an emoji already in its family", snapshot.Name)
		}
		emojis = append(emojis, session.Emoji(snapshot.Name))
	}
}

func TestRevokingShellStopsOnlyLiveChildren(t *testing.T) {
	manager := newTestFamily(t).manager(t, waiting())
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "first"}, {Prompt: "second"}}); err != nil {
		t.Fatal(err)
	}
	names := manager.StopRunning()
	if len(names) != 2 {
		t.Fatalf("stopped names %v", names)
	}
	if again := manager.StopRunning(); len(again) != 0 {
		t.Fatalf("stopped them again: %v", again)
	}
	if _, err := manager.Wait(t.Context(), nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range manager.ListSnapshots() {
		if snapshot.State != subagentrecord.Stopped {
			t.Errorf("%s remains %s", snapshot.Name, snapshot.State)
		}
	}
	notice, isNoticed := subagentrecord.ShellWithdrawnStopNotice(agent.Event{Kind: subagentrecord.ShellWithdrawnStop, Name: strings.Join(names, ",")})
	if !isNoticed || !strings.Contains(notice, strings.Join(names, ", ")) {
		t.Errorf("missing access withdrawal notice %q", notice)
	}
}

func TestAChildRecordsItsConversationInItsOwnJournal(t *testing.T) {
	family := newTestFamily(t)
	probe := tool.Implement(
		tool.Definition{Name: "probe", Description: "probe", Schema: tool.Schema{}},
		func(struct{}) tool.CallRendering { return tool.CallRendering{Subject: "probe"} },
	).Plain(func(context.Context, struct{}) (string, error) { return "verified", nil })
	manager := family.manager(t, func(context.Context, Child) (Worker, error) {
		return Worker{
			Agent:        agent.New("child system", &toolUsingProvider{}, []tool.Tool{probe}),
			Tools:        []tool.Tool{probe},
			SystemPrompt: "child system",
			Close:        func() {},
		}, nil
	})
	if _, err := manager.Start(t.Context(), "shared", []subagent.Task{{Prompt: "investigate"}}); err != nil {
		t.Fatal(err)
	}
	events := untilFinished(t, manager, 1)
	var kinds []agent.Kind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	if !slices.Equal(kinds, []agent.Kind{subagentrecord.Started, subagentrecord.Finished}) {
		t.Fatalf("the parent was handed %v rather than only its facts", kinds)
	}
	if events[1].Text != "verified" || subagentrecord.UsageOf(events[1]) == nil {
		t.Errorf("the finish carries %q and usage %v", events[1].Text, subagentrecord.UsageOf(events[1]))
	}
	name := events[0].Subagent
	stored, err := store.Read(family.directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != events[0].ID {
		t.Errorf("the started fact names session %s, but the child is %s", events[0].ID, stored.ID)
	}
	var childKinds []agent.Kind
	for _, event := range stored.Events {
		childKinds = append(childKinds, event.Kind)
	}
	want := []agent.Kind{caps.ModeChange, agent.UserMessageEvent, agent.ModelReasoningEvent, agent.ToolCallRequestEvent, agent.ToolCallResultEvent, agent.ModelMessageEvent}
	if !slices.Equal(childKinds, want) {
		t.Fatalf("the child journal holds %v, want %v", childKinds, want)
	}
	if stored.Meta.SystemPrompt != "child system" || !slices.Equal(stored.Meta.Tools, []string{"probe"}) {
		t.Errorf("the child froze %q with %v", stored.Meta.SystemPrompt, stored.Meta.Tools)
	}
}

func TestAChildKeepsItsScratchAndItsWorkspaceIsChosenByItsParent(t *testing.T) {
	family := newTestFamily(t)
	prepared := filepath.Join(family.scratch, "job")
	if err := os.Mkdir(prepared, 0o700); err != nil {
		t.Fatal(err)
	}
	var workspaces []string
	var workspacesMutex sync.Mutex
	manager := family.manager(t, func(ctx context.Context, child Child) (Worker, error) {
		workspacesMutex.Lock()
		workspaces = append(workspaces, child.Workspace)
		workspacesMutex.Unlock()
		if err := child.ScratchRoot.WriteFile("work", []byte(child.Name), 0o600); err != nil {
			return Worker{}, err
		}
		return answering(func() agent.Provider { return &answeringProvider{} })(ctx, child)
	})
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "unread", Workspace: "/unreadable"}}); err == nil {
		t.Fatal("a child started in a directory its parent cannot read")
	}
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "here"}, {Prompt: "there", Workspace: prepared}}); err != nil {
		t.Fatal(err)
	}
	events := untilFinished(t, manager, 2)
	for _, event := range events {
		if event.Kind != subagentrecord.Started {
			continue
		}
		origin, _ := subagentrecord.DecodeOrigin(event)
		want := family.workspace
		if event.Text == "there" {
			want = prepared
		}
		if origin.Workspace != want {
			t.Errorf("%s recorded workspace %s, want %s", event.Subagent, origin.Workspace, want)
		}
		written, err := os.ReadFile(filepath.Join(family.scratch, ScratchName, event.Subagent, "work"))
		if err != nil || string(written) != event.Subagent {
			t.Errorf("%s's scratch was not kept: %q, %v", event.Subagent, written, err)
		}
		stored, err := store.Read(family.directory, event.Subagent)
		if err != nil || stored.Meta.WorkspaceDir != want {
			t.Errorf("%s's session is in %+v, %v", event.Subagent, stored, err)
		}
	}
	workspacesMutex.Lock()
	defer workspacesMutex.Unlock()
	if !slices.Contains(workspaces, prepared) || !slices.Contains(workspaces, family.workspace) {
		t.Errorf("children were opened in %v", workspaces)
	}
}

func TestSendResumesAFinishedChildInItsOwnSession(t *testing.T) {
	family := newTestFamily(t)
	manager := family.manager(t, answering(func() agent.Provider { return &answeringProvider{} }))
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "first"}}); err != nil {
		t.Fatal(err)
	}
	name := untilFinished(t, manager, 1)[0].Subagent
	untilSettled(t, manager, name)
	if _, err := manager.Send(t.Context(), "frugal-nobody", "hello"); err == nil {
		t.Error("a follow-up reached a child nobody started")
	}
	if _, err := manager.Send(t.Context(), name, "second"); err != nil {
		t.Fatal(err)
	}
	events := untilFinished(t, manager, 1)
	if events[0].Kind != subagentrecord.Sent || events[0].Text != "second" {
		t.Fatalf("the follow-up was recorded as %+v", events[0])
	}
	if failure := events[len(events)-1].Failure; failure != nil {
		t.Fatalf("the follow-up failed: %s", failure.Text())
	}
	if answer := events[len(events)-1].Text; answer != "first then second" {
		t.Errorf("the resumed child answered %q without its earlier turn", answer)
	}
	stored, err := store.Read(family.directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if stored.TurnCompletions != 2 {
		t.Errorf("the child journal holds %d turns, want 2", stored.TurnCompletions)
	}
}

func TestSendRefusesAChildThatIsRunningGoneOrReplaced(t *testing.T) {
	family := newTestFamily(t)
	running := family.manager(t, waiting())
	if _, err := running.Start(t.Context(), "", []subagent.Task{{Prompt: "wait"}}); err != nil {
		t.Fatal(err)
	}
	name := running.ListSnapshots()[0].Name
	if _, err := running.Send(t.Context(), name, "again"); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Errorf("a follow-up reached a running child: %v", err)
	}

	finished := newTestFamily(t)
	manager := finished.manager(t, answering(func() agent.Provider { return &answeringProvider{} }))
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "first"}, {Prompt: "second"}}); err != nil {
		t.Fatal(err)
	}
	events := untilFinished(t, manager, 2)
	var names []string
	for _, event := range events {
		if event.Kind == subagentrecord.Started {
			names = append(names, event.Subagent)
		}
	}
	for _, name := range names {
		untilSettled(t, manager, name)
	}
	if err := os.RemoveAll(session.Dir(finished.directory, names[0])); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Send(t.Context(), names[0], "again"); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("a follow-up reached a child whose session is gone: %v", err)
	}
	if err := os.Rename(session.Dir(finished.directory, names[1]), session.Dir(finished.directory, names[0])); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Send(t.Context(), names[0], "again"); err == nil || !strings.Contains(err.Error(), "not the one") {
		t.Errorf("a follow-up reached a session that is not the child: %v", err)
	}
}

func TestAnAnswerReadThroughWaitIsMarkedReturnedAfterItsFinish(t *testing.T) {
	manager := newTestFamily(t).manager(t, answering(func() agent.Provider { return &answeringProvider{} }))
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "say hello"}, {Prompt: "say goodbye"}}); err != nil {
		t.Fatal(err)
	}
	snapshots := manager.ListSnapshots()
	first, second := snapshots[0].Name, snapshots[1].Name
	output, err := manager.Wait(t.Context(), []string{first}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "say hello") {
		t.Fatalf("wait returned %q, want the answer", output)
	}
	if !manager.WasReturned(first) || manager.WasReturned(second) {
		t.Fatal("only the child whose answer was read should be returned")
	}
	var kinds []agent.Kind
	for event := range manager.Events() {
		if event.Subagent != first {
			continue
		}
		kinds = append(kinds, event.Kind)
		if event.Kind == subagentrecord.Returned {
			break
		}
	}
	finished := slices.Index(kinds, subagentrecord.Finished)
	returned := slices.Index(kinds, subagentrecord.Returned)
	if finished < 0 || returned < finished {
		t.Errorf("got events %v, want the finish before the return", kinds)
	}
	restored := newTestFamily(t).manager(t, nil)
	restored.Restore([]agent.Event{
		subagentrecord.StartedEvent(first, "id", "say hello", subagentrecord.Origin{}),
		subagentrecord.FinishedEvent(first, subagentrecord.Done, "hello", nil, nil),
		subagentrecord.ReturnedEvent(first),
	})
	if !restored.WasReturned(first) {
		t.Error("a restored manager forgot the answer was returned")
	}
}

func TestAChildLeftRunningEndsWithWhatItsJournalHolds(t *testing.T) {
	family := newTestFamily(t)
	manager := family.manager(t, answering(func() agent.Provider { return &answeringProvider{} }))
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "partial"}, {Prompt: "lost"}}); err != nil {
		t.Fatal(err)
	}
	events := untilFinished(t, manager, 2)
	var started []agent.Event
	for _, event := range events {
		if event.Kind == subagentrecord.Started {
			started = append(started, event)
		}
	}
	if err := os.RemoveAll(session.Dir(family.directory, started[1].Subagent)); err != nil {
		t.Fatal(err)
	}
	endings := Endings(family.directory, started)
	if len(endings) != 2 {
		t.Fatalf("got %d endings for two unfinished children", len(endings))
	}
	if endings[0].Name != string(subagentrecord.Ended) || endings[0].Text != "partial" {
		t.Errorf("the first child ended as %+v", endings[0])
	}
	if usage := subagentrecord.UsageOf(endings[0]); usage == nil || usage.InputTokens != 100 {
		t.Errorf("the first child's usage came back as %+v", usage)
	}
	if usage := subagentrecord.UsageOf(endings[1]); usage != nil {
		t.Errorf("a child whose journal is gone was charged %+v rather than unknown", usage)
	}
}

func TestWithdrawingShellStopsOnlyTheChildrenHoldingOne(t *testing.T) {
	family := newTestFamily(t)
	manager := family.manager(t, waiting())
	*family.caps = caps.Read
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "read"}}); err != nil {
		t.Fatal(err)
	}
	*family.caps = caps.Read | caps.Shell
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "run"}}); err != nil {
		t.Fatal(err)
	}
	holder := manager.ListSnapshots()[1].Name
	if stopped := manager.StopRunning(); !slices.Equal(stopped, []string{holder}) {
		t.Errorf("withdrawing the shell stopped %v, want only %s", stopped, holder)
	}
	if reader := manager.ListSnapshots()[0]; reader.State != subagentrecord.Running {
		t.Errorf("a child with no shell was stopped as %s", reader.State)
	}
}

func TestAChildStoppedJustBeforeItsParentClosesIsRecordedAsStopped(t *testing.T) {
	manager := newTestFamily(t).manager(t, waiting())
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "wait"}}); err != nil {
		t.Fatal(err)
	}
	if stopped := manager.StopRunning(); len(stopped) != 1 {
		t.Fatalf("stopped %v", stopped)
	}
	go manager.Close()
	var finishes []agent.Event
	for event := range manager.Events() {
		if event.Kind == subagentrecord.Finished {
			finishes = append(finishes, event)
		}
	}
	if len(finishes) != 1 || finishes[0].Name != string(subagentrecord.Stopped) {
		t.Errorf("a child stopped before its parent closed finished as %+v", finishes)
	}
}

func TestTheBarIsNeverHeldUpByAStartDoingIO(t *testing.T) {
	family := newTestFamily(t)
	resolving := make(chan struct{})
	release := make(chan struct{})
	manager, err := New(Options{
		Directory: family.directory,
		Scratch:   family.scratch,
		Parent:    parentName,
		Factory:   waiting(),
		PickName:  func(int) int { return 0 },
		Caps:      func() caps.Set { return caps.Read },
		Workspace: func(string) (string, error) {
			close(resolving)
			<-release
			return family.workspace, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	started := make(chan error, 1)
	go func() {
		_, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "slow"}})
		started <- err
	}()
	<-resolving
	listed := make(chan []Snapshot, 1)
	go func() { listed <- manager.ListSnapshots() }()
	select {
	case snapshots := <-listed:
		if len(snapshots) != 0 {
			t.Errorf("a child was listed before it was started: %+v", snapshots)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listing the children waited on a start doing I/O")
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
}

func TestAWaitThatGivesUpSaysWhichSubagentsAreStillRunning(t *testing.T) {
	manager := newTestFamily(t).manager(t, waiting())
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "wait", Intent: "Watching the build"}}); err != nil {
		t.Fatal(err)
	}
	name := manager.ListSnapshots()[0].Name
	report, err := manager.Wait(t.Context(), nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := name + ": running\n\nnote: the wait gave up after 1s, and " + name + " is still running."; report != want {
		t.Errorf("the report %q does not say %q", report, want)
	}
	if manager.WasReturned(name) {
		t.Error("a running child was marked returned by a wait that gave up")
	}
	if intent := manager.ListSnapshots()[0].Intent; intent != "Watching the build" {
		t.Errorf("the child kept its intent as %q", intent)
	}
}

type stallingProvider struct {
	answeringProvider
}

func (self *stallingProvider) Send(ctx context.Context, _ agent.Yield) (agent.Reply, error) {
	<-ctx.Done()
	return agent.Reply{}, ctx.Err()
}

func TestAStoppedChildIsToldWhyWhenItIsResumed(t *testing.T) {
	family := newTestFamily(t)
	manager := family.manager(t, answering(func() agent.Provider { return &stallingProvider{} }))
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "run the tests"}}); err != nil {
		t.Fatal(err)
	}
	name := manager.ListSnapshots()[0].Name
	if stopped := manager.StopRunning(); len(stopped) != 1 {
		t.Fatalf("stopped %v", stopped)
	}
	untilFinished(t, manager, 1)
	untilSettled(t, manager, name)
	stored, err := store.Read(family.directory, name)
	if err != nil {
		t.Fatal(err)
	}
	var history strings.Builder
	for _, item := range stored.Items {
		history.Write(item)
	}
	if want := "Turn stopped because shell execution was withdrawn."; !strings.Contains(history.String(), want) {
		t.Errorf("the child's history does not tell it %q: %s", want, history.String())
	}
}

func TestAFollowUpRunsOnTheModelTheChildStartedOn(t *testing.T) {
	family := newTestFamily(t)
	var opened []Child
	var openedMutex sync.Mutex
	factory := func(ctx context.Context, child Child) (Worker, error) {
		openedMutex.Lock()
		opened = append(opened, child)
		openedMutex.Unlock()
		return answering(func() agent.Provider { return &answeringProvider{} })(ctx, child)
	}
	configured := func(id string, effort string) *Manager {
		choice := model.Choice{Provider: "anthropic", ID: id}
		manager, err := New(Options{
			Directory: family.directory,
			Scratch:   family.scratch,
			Parent:    parentName,
			Choice:    choice,
			Meta:      store.Meta{Provider: "anthropic", Model: id, Effort: effort, ModelChoice: &choice},
			Factory:   factory,
			PickName:  func(int) int { return 0 },
			Caps:      func() caps.Set { return caps.Read },
			Workspace: func(string) (string, error) { return family.workspace, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(manager.Close)
		return manager
	}
	first := configured("claude-opus-5", "high")
	if _, err := first.Start(t.Context(), "", []subagent.Task{{Prompt: "first"}}); err != nil {
		t.Fatal(err)
	}
	events := untilFinished(t, first, 1)
	name := events[0].Subagent
	untilSettled(t, first, name)
	first.Close()

	second := configured("claude-sonnet-5", "low")
	second.Restore(events)
	if _, err := second.Send(t.Context(), name, "again"); err != nil {
		t.Fatal(err)
	}
	untilFinished(t, second, 1)
	openedMutex.Lock()
	defer openedMutex.Unlock()
	if len(opened) != 2 {
		t.Fatalf("opened %d runs, want two", len(opened))
	}
	for _, child := range opened {
		if child.Choice.ID != "claude-opus-5" || child.Selection.Model != "claude-opus-5" || child.Selection.Effort != "high" {
			t.Errorf("a run opened on %+v and %+v, want the model the child started on", child.Choice, child.Selection)
		}
	}
}

func TestLiveSpendPricesEachChildAtItsOwnModel(t *testing.T) {
	manager := newTestFamily(t).manager(t, waiting())
	usage := agent.Usage{InputTokens: 1_000_000}
	manager.mutex.Lock()
	manager.children["frugal-otter"] = &child{
		Snapshot: Snapshot{Name: "frugal-otter", State: subagentrecord.Running},
		usage:    usage,
		cancel:   func() {},
		choice:   model.Choice{Prices: &agent.TokenPrices{Input: 3}},
	}
	manager.children["frugal-heron"] = &child{
		Snapshot: Snapshot{Name: "frugal-heron", State: subagentrecord.Running},
		usage:    usage,
		cancel:   func() {},
		choice:   model.Choice{Prices: &agent.TokenPrices{Input: 15}},
	}
	manager.mutex.Unlock()
	if spend, isPriced := manager.LiveSpend(); !isPriced || spend != 18 {
		t.Errorf("live spend came to %v, priced %t, want 18 from each child's own prices", spend, isPriced)
	}
	manager.mutex.Lock()
	manager.children["frugal-adder"] = &child{
		Snapshot: Snapshot{Name: "frugal-adder", State: subagentrecord.Running},
		usage:    usage,
		cancel:   func() {},
	}
	manager.mutex.Unlock()
	if _, isPriced := manager.LiveSpend(); isPriced {
		t.Error("a running child with no known prices was charged as free")
	}
}

func TestAFollowUpIsToldWhichOfItsToolsChanged(t *testing.T) {
	family := newTestFamily(t)
	change, err := toolset.AvailabilityChangeEvent(
		toolset.Availability{"expose": toolset.ToolAvailable},
		toolset.Availability{"expose": toolset.ToolMissing},
	)
	if err != nil {
		t.Fatal(err)
	}
	provider := &answeringProvider{}
	manager := family.manager(t, func(ctx context.Context, child Child) (Worker, error) {
		worker, err := answering(func() agent.Provider { return provider })(ctx, child)
		if len(child.FrozenTools) > 0 || len(child.History) > 0 {
			worker.Changes = []agent.Event{change}
		}
		return worker, err
	})
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "first"}}); err != nil {
		t.Fatal(err)
	}
	name := untilFinished(t, manager, 1)[0].Subagent
	untilSettled(t, manager, name)
	if _, err := manager.Send(t.Context(), name, "again"); err != nil {
		t.Fatal(err)
	}
	untilFinished(t, manager, 1)
	untilSettled(t, manager, name)

	stored, err := store.Read(family.directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(stored.Events, func(event agent.Event) bool { return event.Kind == toolset.AvailabilityChange }) {
		t.Error("the child's journal does not record its tools changing")
	}
	if !strings.Contains(strings.Join(provider.prompts, "\n"), "expose") {
		t.Errorf("the child was not told its tools changed: %v", provider.prompts)
	}
}

func TestAManagerNamesItsModelAsPeopleKnowIt(t *testing.T) {
	for _, run := range []struct {
		choice model.Choice
		want   string
	}{
		{model.Choice{ID: "claude-opus-5", Name: "Claude Opus 5"}, "Claude Opus 5"},
		{model.Choice{ID: "local-model"}, "local-model"},
	} {
		choice, want := run.choice, run.want
		manager, err := New(Options{Directory: t.TempDir(), Scratch: t.TempDir(), Choice: choice})
		if err != nil {
			t.Fatal(err)
		}
		if got := manager.Model(); got != want {
			t.Errorf("%+v was named %q, want %q", choice, got, want)
		}
		manager.Close()
	}
}

func TestAReportNamesWhereItsChildWrites(t *testing.T) {
	family := newTestFamily(t)
	manager, err := New(Options{
		Directory:   family.directory,
		Scratch:     family.scratch,
		Parent:      parentName,
		Factory:     answering(func() agent.Provider { return &answeringProvider{} }),
		PickName:    func(int) int { return 0 },
		Caps:        func() caps.Set { return caps.Read },
		Workspace:   func(string) (string, error) { return family.workspace, nil },
		ScratchNote: func(name string) string { return "its /tmp is your /tmp/subagents/" + name },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Prompt: "hello"}}); err != nil {
		t.Fatal(err)
	}
	name := untilFinished(t, manager, 1)[0].Subagent
	untilSettled(t, manager, name)
	report, err := manager.Output(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := name + ": done (its /tmp is your /tmp/subagents/" + name + ")\nhello"; report != want {
		t.Errorf("the report read %q, want %q", report, want)
	}
}
