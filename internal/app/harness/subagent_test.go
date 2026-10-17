package harness

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/subagentCounts"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/subagent"
)

func TestGoldenSubagentBarSummarisesEveryState(t *testing.T) {
	at := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	states := []subagents.Snapshot{
		{Name: "tame-adder", State: subagentrecord.Running, ActivityAt: at},
		{Name: "tame-alpaca", State: subagentrecord.Stopping},
		{Name: "tame-anchovy", State: subagentrecord.Done, EndedAt: at.Add(-time.Second)},
		{Name: "tame-badger", State: subagentrecord.Failed, EndedAt: at.Add(-time.Second)},
	}
	render := func(now time.Time, snapshots []subagents.Snapshot) string {
		factory := subagentCounts.New(func() []subagents.Snapshot { return snapshots }, func() time.Time { return now })
		built, err := factory(nil)
		if err != nil {
			t.Fatal(err)
		}
		return built.Render(segment.Context{})
	}
	passes := map[string]func() string{
		"one running one stopping one done one failed": func() string { return render(at, states) },
		"flash expires but running continues":          func() string { return render(at.Add(time.Second), states) },
		"idle after outcomes expire":                   func() string { return render(at.Add(31*time.Second), states[2:]) },
		"just finished child stays hidden":             func() string { return render(at, states[2:3]) },
		"restored ended child stays hidden": func() string {
			return render(at, []subagents.Snapshot{{Name: "tame-adder", State: subagentrecord.Ended}})
		},
	}
	compareWithGolden(t, "subagent-bar", ".ansi", passes)
	compareWithGolden(t, "subagent-bar", ".screen", shownPasses(t, passes))
}

func TestGoldenSubagentEventsDrawCompactlyAndReplay(t *testing.T) {
	live := func(events []agent.Event) func() string {
		return func() string {
			var drawn bytes.Buffer
			conversation := testConversation(t, &drawn)
			conversation.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			conversation.currentTurn = Turn{painter: conversation.newPainter(true)}
			for _, event := range events {
				if event.Subagent != "" {
					conversation.subagentEvent(event)
					continue
				}
				conversation.recordEvent(event)
			}
			conversation.currentTurn.painter.Close(dynamic.Done)
			conversation.screen.End()
			return drawn.String()
		}
	}
	replayed := func(events []agent.Event) func() string {
		return func() string {
			var drawn bytes.Buffer
			conversation := testConversation(t, &drawn)
			conversation.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			conversation.recordedEvents = events
			conversation.replay()
			return drawn.String()
		}
	}
	restoredPending := func(events ...agent.Event) func() string {
		return func() string {
			var drawn bytes.Buffer
			conversation := testConversation(t, &drawn)
			conversation.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			conversation.children.directory = t.TempDir()
			conversation.recordedEvents = events
			conversation.restoreChildren(conversation.recordedEvents)
			conversation.replay()
			conversation.refreshPendingMessages()
			return drawn.String()
		}
	}
	passes := map[string]func() string{
		"message live":   live(childMessageFixtureEvents()),
		"message replay": replayed(childMessageFixtureEvents()),
		"restored pending message": restoredPending(
			subagentrecord.ReceivedEvent("tame-adder", "Halfway through the README."),
			finished("tame-adder", "The README says hello."),
		),
		"restored pending message already delivered": restoredPending(
			subagentrecord.ReceivedEvent("tame-adder", "Halfway through the README."),
			subagentrecord.DeliveryEvent([]string{"tame-adder"}, "Subagent tame-adder interjects:\n\nHalfway through the README.", []subagentrecord.Report{
				{Name: "tame-adder", Message: "Halfway through the README."},
			}),
			finished("tame-adder", "The README says hello."),
		),
		"live":             live(childFixtureEvents()),
		"replay":           replayed(childFixtureEvents()),
		"restored pending": restoredPending(finished("tame-adder", "The README says hello.")),
	}
	compareWithGolden(t, "subagent-timeline", ".ansi", passes)
	compareWithGolden(t, "subagent-timeline", ".screen", shownPasses(t, passes))
}

func childFixtureEvents() []agent.Event {
	origin := subagentrecord.Origin{Workspace: sessionGoldenWorkspace}
	return []agent.Event{
		subagentrecord.StartedEvent("tame-adder", "child-1", "Find the issue", origin),
		subagentrecord.StartedEvent("tame-alpaca", "child-2", "Check the tests", origin),
		subagentrecord.FinishedEvent("tame-adder", subagentrecord.Done, "The README says hello.", nil, &agent.Usage{InputTokens: 10}),
		subagentrecord.AccessWithdrawnStopEvent([]string{"tame-alpaca"}, caps.Shell),
		subagentrecord.FinishedEvent("tame-alpaca", subagentrecord.Stopped, "", nil, &agent.Usage{}),
		subagentrecord.DeliveryEvent([]string{"tame-adder"}, "Subagent tame-adder finished:\n\ntame-adder done: The README says hello.", []subagentrecord.Report{
			{Name: "tame-adder", State: subagentrecord.Done, Answer: "The README says hello."},
		}),
	}
}

func childMessageFixtureEvents() []agent.Event {
	return []agent.Event{
		subagentrecord.StartedEvent("tame-adder", "child-1", "Find the issue", subagentrecord.Origin{Workspace: sessionGoldenWorkspace}),
		subagentrecord.ReceivedEvent("tame-adder", "Halfway through the README."),
		subagentrecord.FinishedEvent("tame-adder", subagentrecord.Done, "The README says hello.", nil, &agent.Usage{InputTokens: 10}),
		subagentrecord.DeliveryEvent(
			[]string{"tame-adder"},
			"Subagent tame-adder interjects:\n\nHalfway through the README.\n\nSubagent tame-adder finished:\n\ntame-adder done: The README says hello.",
			[]subagentrecord.Report{
				{Name: "tame-adder", Message: "Halfway through the README."},
				{Name: "tame-adder", State: subagentrecord.Done, Answer: "The README says hello."},
			},
		),
	}
}

func finished(name string, answer string) agent.Event {
	return subagentrecord.FinishedEvent(name, subagentrecord.Done, answer, nil, &agent.Usage{})
}

func TestCompletionsOpenOneFixedWindowAndWakeOneTurn(t *testing.T) {
	var drawn bytes.Buffer
	conversation := testConversation(t, &drawn)
	current := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	conversation.now = func() time.Time { return current }
	conversation.subagentEvent(finished("tame-adder", "first"))
	firstDeadline := conversation.children.deliveryAt
	current = current.Add(3 * time.Second)
	conversation.subagentEvent(finished("tame-alpaca", "second"))
	if !conversation.children.deliveryAt.Equal(firstDeadline) || len(conversation.children.reportsDue) != 2 {
		t.Fatalf("second completion moved the first deadline: %v", conversation.children.deliveryAt)
	}
	conversation.deliverChildCompletions()
	if conversation.currentTurn.Running() {
		t.Fatal("subagent turn began before its debounce window closed")
	}
	current = firstDeadline
	conversation.deliverChildCompletions()
	if !conversation.currentTurn.Running() || len(conversation.children.reportsDue) != 0 {
		t.Fatal("subagent completions did not start exactly one turn")
	}
	for report := range conversation.currentTurn.Events() {
		conversation.takeTurn(report)
	}
	conversation.finish()
	var submitted []agent.Event
	for _, event := range conversation.recordedEvents {
		if event.Kind == subagentrecord.ReportsDelivered {
			submitted = append(submitted, event)
		}
	}
	if len(submitted) != 1 || submitted[0].Name != "tame-adder,tame-alpaca" {
		t.Errorf("wrote %d completion batches: %+v", len(submitted), submitted)
	}
}

func TestReadyChildrenJoinTheNextUserTurn(t *testing.T) {
	var drawn bytes.Buffer
	conversation := testConversation(t, &drawn)
	current := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	conversation.now = func() time.Time { return current }
	conversation.recordedEvents = []agent.Event{finished("tame-adder", "child answer")}
	conversation.children.reportsDue = conversation.recordedEvents
	conversation.children.deliveryAt = current
	conversation.start("new question")
	for report := range conversation.currentTurn.Events() {
		conversation.takeTurn(report)
	}
	conversation.finish()
	var notices, questions int
	for _, event := range conversation.recordedEvents {
		if event.Kind == subagentrecord.ReportsDelivered {
			notices++
			if !strings.Contains(event.Text, "child answer") {
				t.Errorf("the delivery lost the answer: %q", event.Text)
			}
		}
		if event.Kind == agent.UserMessageEvent {
			questions++
		}
	}
	if notices != 1 || questions != 1 || len(conversation.children.reportsDue) != 0 {
		t.Fatalf("completion did not join the user turn: notices=%d questions=%d", notices, questions)
	}
}

func TestARestoredSubagentReportWaitsForTheNextTurn(t *testing.T) {
	var drawn bytes.Buffer
	conversation := testConversation(t, &drawn)
	conversation.children.directory = t.TempDir()
	conversation.recordedEvents = []agent.Event{finished("tame-adder", "child answer")}
	conversation.restoreChildren(conversation.recordedEvents)
	conversation.deliverChildCompletions()

	if conversation.currentTurn.Running() || len(conversation.children.reportsDue) != 0 {
		t.Fatal("restoring a report started a turn or scheduled a wake-up")
	}
	if len(conversation.pendingNotices.items) != 1 || conversation.pendingNotices.items[0].state.Kind != subagentrecord.ReportsDelivered {
		t.Fatalf("restored report was not pending: %+v", conversation.pendingNotices.items)
	}
	if len(conversation.recordedEvents) != 1 {
		t.Fatal("pending report was recorded before being sent")
	}
	conversation.start("carry on")
	for event := range conversation.currentTurn.Events() {
		conversation.takeTurn(event)
	}
	conversation.finish()
	if len(conversation.pendingNotices.items) != 0 || len(conversation.recordedEvents) < 2 ||
		conversation.recordedEvents[1].Kind != subagentrecord.ReportsDelivered ||
		!strings.Contains(conversation.recordedEvents[1].Text, "child answer") {
		t.Fatalf("next turn did not submit the pending report: %+v", conversation.recordedEvents)
	}
}

func TestSubagentCompletionBatchesDistinctResults(t *testing.T) {
	first := []agent.Event{finished("tame-adder", "first answer"), finished("tame-alpaca", "second answer")}
	message := completionMessage(first, func(name string) string { return " (scratch of " + name + ")" })
	if !strings.Contains(message, "tame-adder done (scratch of tame-adder): first answer") || !strings.Contains(message, "second answer") {
		t.Fatalf("batch omitted a result: %q", message)
	}
	conversation := &App{now: time.Now}
	conversation.queueUndeliveredReports(first)
	if len(conversation.children.reportsDue) != 2 {
		t.Errorf("unsent completions lost: %v", conversation.children.reportsDue)
	}
	first = append(first, agent.Event{Kind: subagentrecord.ReportsDelivered, Name: "tame-adder,tame-alpaca", Text: message})
	conversation = &App{now: time.Now}
	conversation.queueUndeliveredReports(first)
	if len(conversation.children.reportsDue) != 0 {
		t.Errorf("submitted completions replayed: %v", conversation.children.reportsDue)
	}
}

func TestAMidRunMessageJoinsTheBatchAheadOfTheFinishes(t *testing.T) {
	due := []agent.Event{
		subagentrecord.ReceivedEvent("tame-adder", "Is the parser in scope?"),
		finished("tame-alpaca", "second answer"),
	}
	message := completionMessage(due, func(string) string { return "" })
	want := "Subagent tame-adder interjects:\n\nIs the parser in scope?\n\n" +
		"Subagent tame-alpaca finished:\n\ntame-alpaca done: second answer"
	if message != want {
		t.Errorf("told the parent %q, want %q", message, want)
	}
	reports := completionReports(due)
	if len(reports) != 2 || !reports[0].IsMessage() || reports[0].Message != "Is the parser in scope?" || reports[1].IsMessage() {
		t.Errorf("reported %+v", reports)
	}
	if notice := reports[0].Notice(); notice != "Subagent tame-adder interjects:\n\nIs the parser in scope?" {
		t.Errorf("drew %q", notice)
	}
}

func TestOnlyTheMessagesADeliveryCarriedCountAsDelivered(t *testing.T) {
	firstMessage := subagentrecord.ReceivedEvent("tame-adder", "first")
	secondMessage := subagentrecord.ReceivedEvent("tame-adder", "second")
	delivery := subagentrecord.DeliveryEvent([]string{"tame-adder"}, "Subagent tame-adder interjects:\n\nfirst", []subagentrecord.Report{
		{Name: "tame-adder", Message: "first"},
	})
	conversation := &App{now: time.Now}
	conversation.queueUndeliveredReports([]agent.Event{firstMessage, secondMessage, delivery, finished("tame-adder", "answer")})
	due := conversation.children.reportsDue
	if len(due) != 2 || due[0].Text != "second" || due[1].Kind != subagentrecord.Finished {
		t.Errorf("left due %+v", due)
	}
}

func TestAChildLeftRunningWhenTheParentStopsIsNotResumed(t *testing.T) {
	directory := t.TempDir()
	events := []agent.Event{subagentrecord.StartedEvent("tame-adder", "child-1", "inspect", subagentrecord.Origin{})}
	ended := subagents.Endings(directory, events)
	if len(ended) != 1 || ended[0].Name != string(subagentrecord.Ended) || ended[0].Subagent != "tame-adder" {
		t.Fatalf("missing ended status: %+v", ended)
	}
	if usage := subagentrecord.UsageOf(ended[0]); usage != nil {
		t.Errorf("a child with no journal was charged %+v rather than unknown", usage)
	}
	if again := subagents.Endings(directory, append(events, ended...)); len(again) != 0 {
		t.Fatalf("child ended twice on resume: %+v", again)
	}
}

func TestAnUnpricedChildMakesTheSessionTotalUnknown(t *testing.T) {
	conversation := &App{metrics: metrics.New(metrics.Settings{Prices: &agent.TokenPrices{Input: 2}})}
	conversation.children.spend.record(subagentrecord.StartedEvent("tame-adder", "child-1", "inspect", subagentrecord.Origin{
		Choice: model.Choice{Provider: "ollama", ID: "local"},
	}))
	conversation.children.spend.record(subagentrecord.FinishedEvent(
		"tame-adder", subagentrecord.Done, "", nil, &agent.Usage{InputTokens: 12},
	))
	if _, isPriced := conversation.sessionSpend(); isPriced {
		t.Fatal("an unpriced subagent was charged as free")
	}
}

func TestAChildWhoseUsageIsUnknownMakesTheSessionTotalUnknown(t *testing.T) {
	conversation := &App{metrics: metrics.New(metrics.Settings{Prices: &agent.TokenPrices{Input: 2}})}
	conversation.children.spend.record(subagentrecord.StartedEvent("tame-adder", "child-1", "inspect", subagentrecord.Origin{
		Choice: model.Choice{Prices: &agent.TokenPrices{Input: 12}},
	}))
	conversation.children.spend.record(subagentrecord.FinishedEvent("tame-adder", subagentrecord.Ended, "", nil, nil))
	if _, isPriced := conversation.sessionSpend(); isPriced {
		t.Fatal("a child whose usage was lost was charged as free")
	}
}

func TestSubagentUsageUsesItsOwnPrice(t *testing.T) {
	conversation := &App{metrics: metrics.New(metrics.Settings{Prices: &agent.TokenPrices{Input: 2}})}
	conversation.children.spend.record(subagentrecord.StartedEvent("tame-adder", "child-1", "inspect", subagentrecord.Origin{
		Choice: model.Choice{Prices: &agent.TokenPrices{Input: 12}},
	}))
	conversation.children.spend.record(subagentrecord.FinishedEvent(
		"tame-adder", subagentrecord.Done, "", nil, &agent.Usage{InputTokens: 1_000_000},
	))
	cost, known := conversation.sessionSpend()
	if !known || cost != 12 {
		t.Fatalf("session spend %f, known=%t", cost, known)
	}
}

func TestGoldenAChildStoppedWhileIdleWaitsBesideTheModeChange(t *testing.T) {
	passes := map[string]func() string{
		"one running child": func() string { return childStoppedWhileIdleStream(t, 1) },
		"two running children": func() string {
			return childStoppedWhileIdleStream(t, 2)
		},
	}
	compareWithGolden(t, "subagent-pending", ".ansi", passes)
	compareWithGolden(t, "subagent-pending", ".screen", shownPasses(t, passes))
}

func childStoppedWhileIdleStream(t *testing.T, childCount int) string {
	t.Helper()

	self, _ := modeFixture(t)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.children.manager = waitingChildManager(t)
	tasks := slices.Repeat([]subagent.Task{{Model: "anthropic/claude-opus-5", Prompt: "Watch the build"}}, childCount)
	if _, err := self.children.manager.Start(t.Context(), "", tasks); err != nil {
		t.Fatal(err)
	}
	inputLine := edit.NewInput(nil)
	self.show(inputLine)
	self.toggleCap(caps.Shell)
	self.show(inputLine)
	return screenOutput.String()
}

func waitingChildManager(t *testing.T) *subagents.Manager {
	t.Helper()

	manager, err := subagents.New(subagents.Options{
		Models:    []subagents.ChildModel{{Choice: model.Choice{Provider: "anthropic", ID: "claude-opus-5"}, Selection: model.Selection{Provider: "anthropic", Model: "claude-opus-5"}}},
		Directory: t.TempDir(),
		Scratch:   t.TempDir(),
		Parent:    goldenSessionName,
		Factory: func(context.Context, subagents.Child) (subagents.Worker, error) {
			return subagents.Worker{Agent: agent.New("", unaskedProvider{}, nil), Close: func() {}}, nil
		},
		PickName:  func(int) int { return 0 },
		Workspace: func(string) (string, error) { return sessionGoldenWorkspace, nil },
		Caps:      func() caps.Set { return caps.Read | caps.Shell },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func TestAFailedPagerIsNamedAsThePager(t *testing.T) {
	for _, run := range []struct {
		viewed string
		want   string
	}{
		{"", "The editor failed: exit status 2"},
		{filepath.Join(t.TempDir(), "conversation.txt"), "The pager failed: exit status 2"},
	} {
		var drawn bytes.Buffer
		conversation := testConversation(t, &drawn)
		conversation.children.viewedConversation = run.viewed
		conversation.editorEnded(editor.Outcome{IsAwaited: true, Failure: errors.New("exit status 2")})
		if got := conversation.feedback.Message().Text; got != run.want {
			t.Errorf("a failure was told as %q, want %q", got, run.want)
		}
		if conversation.children.viewedConversation != "" {
			t.Error("the conversation drawn for the pager was not forgotten")
		}
	}
}

func TestGoldenSubCatDrawsTheChildsConversation(t *testing.T) {
	directory := session.ChildrenDir(t.TempDir(), goldenSessionName)
	writer, err := store.CreateNamed(directory, "tame-adder", store.Meta{WorkspaceDir: sessionGoldenWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "read", Arguments: `{"path":"README.md"}`}
	request.SetRendering(tool.CallRendering{Subject: "README.md"})
	for _, event := range []agent.Event{
		caps.ModeEvent(caps.Read),
		{Kind: agent.UserMessageEvent, Text: "Read the README and quote it"},
		{Kind: agent.ModelReasoningEvent, Text: "I should read the README."},
		request,
		{Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "read", Text: "hello", Status: agent.SuccessStatus},
		{Kind: agent.ModelMessageEvent, Text: "The README says hello, and holds nothing else beyond its one heading."},
		caps.ModeToggleEvent(caps.Shell, caps.Read|caps.Shell),
		{Kind: agent.UserMessageEvent, Text: "Now quote it twice"},
		{Kind: agent.ModelMessageEvent, Text: "hello hello"},
	} {
		if err := writer.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	draw := func(name string, columns int) func() string {
		return func() string {
			var drawn bytes.Buffer
			conversation := testConversation(t, &drawn)
			conversation.children.directory = directory
			rows, err := conversation.drawSubagent(name, columns)
			if err != nil {
				return "error: " + err.Error() + "\n"
			}
			return strings.Join(rows, "\n") + "\n"
		}
	}
	passes := map[string]func() string{
		"wide":    draw("tame-adder", replayColumns),
		"narrow":  draw("tame-adder", 40),
		"missing": draw("tame-alpaca", replayColumns),
	}
	compareWithGolden(t, "subagent-conversation", ".ansi", passes)
	compareWithGolden(t, "subagent-conversation", ".screen", shownPasses(t, passes))
}

type promptlyAnsweringProvider struct{ unaskedProvider }

func (promptlyAnsweringProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "done"})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})
	return agent.Reply{}, nil
}

func TestAReportIsInterjectedIntoARunningTurnEvenOnceItWasRead(t *testing.T) {
	var drawn bytes.Buffer
	conversation := testConversation(t, &drawn)
	current := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	conversation.now = func() time.Time { return current }
	scratch := t.TempDir()
	manager, err := subagents.New(subagents.Options{
		Models:    []subagents.ChildModel{{Choice: model.Choice{Provider: "anthropic", ID: "claude-opus-5"}, Selection: model.Selection{Provider: "anthropic", Model: "claude-opus-5"}}},
		Directory: filepath.Join(t.TempDir(), "subagents"),
		Scratch:   scratch,
		Parent:    "tame-impala",
		Factory: func(_ context.Context, child subagents.Child) (subagents.Worker, error) {
			worker := agent.New("child", promptlyAnsweringProvider{}, nil)
			return subagents.Worker{Agent: worker, SystemPrompt: "child", Close: func() {}}, nil
		},
		PickName:  func(int) int { return 0 },
		Workspace: func(string) (string, error) { return scratch, nil },
		Caps:      func() caps.Set { return caps.Read },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	conversation.children.manager = manager
	if _, err := manager.Start(t.Context(), "", []subagent.Task{{Model: "anthropic/claude-opus-5", Prompt: "one"}, {Model: "anthropic/claude-opus-5", Prompt: "two"}}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for finishes := 0; finishes < 2; {
		event := <-manager.Events()
		if event.Kind == subagentrecord.Started {
			names = append(names, event.Subagent)
		}
		if event.Kind == subagentrecord.Finished {
			finishes++
		}
		conversation.subagentEvent(event)
	}
	if _, err := manager.Output(names[:1]); err != nil {
		t.Fatal(err)
	}
	conversation.currentTurn = Turn{painter: conversation.newPainter(true), Stream: testRunningTurnStreamWithCancel(nil)}
	current = conversation.children.deliveryAt
	conversation.deliverChildCompletions()

	notes, isNoted := conversation.currentTurn.TakeNotes()
	if !isNoted || len(notes) != 1 || !strings.Contains(notes[0].Text, names[0]) || !strings.Contains(notes[0].Text, names[1]) {
		t.Fatalf("the running turn was told %+v, want both reports, read or waited on as they were", notes)
	}
	if len(conversation.children.reportsDue) != 0 {
		t.Errorf("reports still due are %v after they were interjected", conversation.children.reportsDue)
	}
	if !slices.ContainsFunc(conversation.recordedEvents, func(event agent.Event) bool {
		return event.Kind == subagentrecord.ReportsDelivered
	}) {
		t.Error("the interjected reports were not recorded")
	}
}

func TestReloadingConfigChangesTheSubagentLimitForTheNextStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[subagent]\nconcurrency = 2\n")

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)
	choice := model.Choice{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}}
	rotate := func(settings config.Subagent, defaults model.Defaults) (subagents.Rotation, error) {
		return childRotationOf(choice, settings, defaults, []model.Choice{choice})
	}
	initial, err := rotate(config.Subagent{Concurrency: 2}, model.Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := subagents.New(subagents.Options{
		Directory:   filepath.Join(t.TempDir(), "subagents"),
		Scratch:     t.TempDir(),
		Parent:      "tame-impala",
		Models:      initial.Models,
		Concurrency: initial.Concurrency,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	self.children = childState{manager: manager, rotate: rotate}

	writeLiveConfig(t, path, "[subagent]\nconcurrency = 7\n")
	settleLiveConfig(t, self)
	if got := manager.Concurrency(); got != 7 {
		t.Errorf("the limit after a reload is %d, want 7", got)
	}
	if got := manager.Model(); got != choice.ID {
		t.Errorf("the automatic pool after a reload names %q, want %q", got, choice.ID)
	}

	writeLiveConfig(t, path, "[subagent]\nconcurrency = 0\n")
	settleLiveConfig(t, self)
	if got := manager.Concurrency(); got != 7 || manager.Model() != choice.ID {
		t.Errorf("an invalid limit replaced the one standing: %d, %q", got, manager.Model())
	}
	if message := self.feedback.Message(); message.Status != agent.ErrorStatus || !strings.Contains(message.Text, "subagent.concurrency") {
		t.Errorf("an invalid limit said %+v", message)
	}
}

func TestAnEmptySubagentCatalogueKeepsItsConcurrency(t *testing.T) {
	rotation, err := childRotationOf(model.Choice{}, config.Subagent{Concurrency: 4}, model.Defaults{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rotation.Concurrency != 4 || len(rotation.Models) != 0 {
		t.Fatalf("an empty rotation came back as %+v", rotation)
	}
}

func TestAnOllamaSessionStartsSubagentsOnlyOnOllamaModels(t *testing.T) {
	local := model.Choice{Provider: model.OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k", EffortLevels: []string{"none"}}
	other := model.Choice{Provider: model.OllamaProvider, ID: "muse-glimmer-128k:latest", EffortLevels: []string{"none"}}
	hosted := model.Choice{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}}
	choices := []model.Choice{local, other, hosted}
	for _, test := range []struct {
		parent model.Choice
		want   []string
	}{
		{local, []string{"qwen3.8:27b-mtp-q8_0-256k", "muse-glimmer-128k:latest"}},
		{hosted, []string{"claude-haiku-5-5"}},
	} {
		rotation, err := childRotationOf(test.parent, config.Subagent{Concurrency: 4}, model.Defaults{}, choices)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, child := range rotation.Models {
			got = append(got, child.Choice.ID)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("a session on %s rotates over %v, want %v", test.parent.Provider, got, test.want)
		}
	}
}

func TestASessionCanStartChildrenFromTheCatalogueWithoutARotation(t *testing.T) {
	local := model.Choice{Provider: model.OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k", EffortLevels: []string{"none"}}
	hosted := model.Choice{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}}
	choices := []model.Choice{local, hosted}
	for _, test := range []struct {
		name     string
		parent   model.Choice
		settings config.Config
		want     bool
	}{
		{"no configured subagent model", hosted, config.Config{}, true},
		{"an ollama session keeps its local pool", local, config.Config{}, true},
		{"a concurrency limit keeps the automatic pool", hosted, config.Config{Subagent: config.Subagent{Concurrency: 2}}, true},
		{"a hosted session cannot use local or another provider's paid models", model.Choice{Provider: model.CodexProvider}, config.Config{}, false},
	} {
		if got := canStartChildren(test.parent, test.settings, choices); got != test.want {
			t.Errorf("%s: canStartChildren is %t, want %t", test.name, got, test.want)
		}
	}
}

func TestGoldenSubagentProviderPools(t *testing.T) {
	choices := []model.Choice{
		{Provider: model.CodexProvider, ID: "gpt-6.1-sol", EffortLevels: []string{"high"}},
		{Provider: model.CodexProvider, ID: "gpt-6-luna", EffortLevels: []string{"high"}},
		{Provider: model.AnthropicProvider, ID: "claude-opus-5-5", EffortLevels: []string{"high"}},
		{Provider: model.AnthropicProvider, ID: "claude-sonnet-5-5", EffortLevels: []string{"high"}},
		{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "deepseek-v4.1-flash", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "gpt-6-luna", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "longcat-2.5-preview-free"},
		{Provider: model.OpencodeGoProvider, ID: "step-5-preview-free", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "cat-jam-free", EffortLevels: []string{"high"}},
		{Provider: model.OllamaProvider, ID: "muse-glimmer-128k:latest", EffortLevels: []string{"high"}},
		{Provider: model.OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k", EffortLevels: []string{"high"}},
	}
	passes := map[string]func() string{}
	for _, provider := range model.ProviderNames() {
		passes[provider] = func() string {
			rotation, err := childRotationOf(model.Choice{Provider: provider}, config.Subagent{}, model.Defaults{}, choices)
			if err != nil {
				t.Fatal(err)
			}
			manager, err := subagents.New(subagents.Options{Directory: t.TempDir(), Scratch: t.TempDir(), Models: rotation.Models})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			return manager.Models()
		}
	}
	compareWithGolden(t, "subagent-provider-pools", ".txt", passes)
}

func TestSubagentCatalogueIgnoresFastDefaults(t *testing.T) {
	choice := model.Choice{Provider: model.CodexProvider, ID: "gpt-6-luna", EffortLevels: []string{"high"}}
	rotation, err := childRotationOf(choice, config.Subagent{}, model.Defaults{IsFast: true}, []model.Choice{choice})
	if err != nil || len(rotation.Models) != 1 || rotation.Models[0].Selection.IsFast {
		t.Errorf("fast defaults reached the subagent catalogue: %+v, %v", rotation, err)
	}
}
