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
		{Name: "tame-adder", State: subagentrecord.Running},
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
		"idle after outcomes expire":                   func() string { return render(at.Add(31*time.Second), states[2:]) },
		"restored ended child stays hidden": func() string {
			return render(at, []subagents.Snapshot{{Name: "tame-adder", State: subagentrecord.Ended}})
		},
	}
	compareWithGolden(t, "subagent-bar", ".ansi", passes)
	compareWithGolden(t, "subagent-bar", ".screen", shownPasses(t, passes))
}

func TestGoldenSubagentEventsDrawCompactlyAndReplay(t *testing.T) {
	passes := map[string]func() string{
		"live": func() string {
			var drawn bytes.Buffer
			conversation := testConversation(t, &drawn)
			conversation.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			conversation.currentTurn = Turn{painter: conversation.newPainter(true)}
			for _, event := range childFixtureEvents() {
				if event.Subagent != "" {
					conversation.subagentEvent(event)
					continue
				}
				conversation.recordEvent(event)
			}
			conversation.currentTurn.painter.Close(dynamic.Done)
			conversation.screen.End()
			return drawn.String()
		},
		"replay": func() string {
			var drawn bytes.Buffer
			conversation := testConversation(t, &drawn)
			conversation.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			conversation.recordedEvents = childFixtureEvents()
			conversation.replay()
			return drawn.String()
		},
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
		{Kind: subagentrecord.ShellWithdrawnStop, Name: "tame-alpaca"},
		subagentrecord.FinishedEvent("tame-alpaca", subagentrecord.Stopped, "", nil, &agent.Usage{}),
		subagentrecord.DeliveryEvent([]string{"tame-adder"}, "Subagent tame-adder finished:\n\ntame-adder done: The README says hello.", []subagentrecord.Report{
			{Name: "tame-adder", State: subagentrecord.Done, Answer: "The README says hello."},
		}),
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
	conversation.children.reportsDue = []string{"tame-adder"}
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

func TestSubagentCompletionBatchesDistinctResults(t *testing.T) {
	first := []agent.Event{finished("tame-adder", "first answer"), finished("tame-alpaca", "second answer")}
	message := completionMessage(first, []string{"tame-adder", "tame-alpaca"}, func(name string) string { return " (scratch of " + name + ")" })
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
	tasks := slices.Repeat([]subagent.Task{{Prompt: "Watch the build"}}, childCount)
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
