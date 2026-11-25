package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/feedback"
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
	renderFor := func(sessionName string, now time.Time, snapshots []subagents.Snapshot) string {
		factory := subagentCounts.New(
			sessionName,
			func() []subagents.Snapshot { return snapshots },
			func() time.Time { return now },
		)
		built, err := factory(nil)
		if err != nil {
			t.Fatal(err)
		}
		return built.Render(segment.Context{})
	}
	render := func(now time.Time, snapshots []subagents.Snapshot) string {
		return renderFor("tame-impala", now, snapshots)
	}
	passes := map[string]func() string{
		"one running one stopping one done one failed": func() string { return render(at, states) },
		"unknown child uses linked fallback mark": func() string {
			return render(at, []subagents.Snapshot{{Name: "worker", State: subagentrecord.Running, ActivityAt: at}})
		},
		"missing session leaves child unlinked": func() string {
			return renderFor("", at, []subagents.Snapshot{{Name: "tame-adder", State: subagentrecord.Running, ActivityAt: at}})
		},
		"flash expires but running continues": func() string { return render(at.Add(time.Second), states) },
		"idle after outcomes expire":          func() string { return render(at.Add(31*time.Second), states[2:]) },
		"just finished child stays hidden":    func() string { return render(at, states[2:3]) },
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
			conversation.recorder = namedGoldenRecorder(t)
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
			conversation.recorder = namedGoldenRecorder(t)
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
			conversation.recorder = namedGoldenRecorder(t)
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

var progressReport = regexp.MustCompile(`\x1b\]9;4;[0-9]\x1b\\`)

func progressReports(drawn string) []string {
	return progressReport.FindAllString(drawn, -1)
}

func TestTheTerminalStaysBusyUntilEveryChildIsDoneAndReported(t *testing.T) {
	const (
		busy = "\x1b]9;4;3\x1b\\"
		idle = "\x1b]9;4;0\x1b\\"
	)
	var drawn bytes.Buffer
	conversation := testConversation(t, &drawn)
	var screenOutput strings.Builder
	conversation.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	current := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	conversation.now = func() time.Time { return current }

	completeTurn(conversation)
	if got, want := progressReports(screenOutput.String()), []string{busy, idle}; !slices.Equal(got, want) {
		t.Fatalf("a turn without children reported %q, want %q", got, want)
	}
	screenOutput.Reset()

	conversation.start("watch the build and the tests")
	conversation.subagentEvent(subagentrecord.StartedEvent("tame-adder", "adder", "Watch the build", subagentrecord.Origin{}))
	conversation.subagentEvent(subagentrecord.StartedEvent("tame-alpaca", "alpaca", "Watch the tests", subagentrecord.Origin{}))
	for report := range conversation.currentTurn.Events() {
		conversation.takeTurn(report)
	}
	conversation.finish()
	if got, want := progressReports(screenOutput.String()), []string{busy}; !slices.Equal(got, want) {
		t.Fatalf("a turn ending with children running reported %q, want %q", got, want)
	}

	conversation.subagentEvent(subagentrecord.FinishedEvent("tame-alpaca", subagentrecord.Stopped, "", nil, &agent.Usage{}))
	conversation.subagentEvent(finished("tame-adder", "the build passed"))
	if got, want := progressReports(screenOutput.String()), []string{busy}; !slices.Equal(got, want) {
		t.Fatalf("a report waiting for delivery reported %q, want %q", got, want)
	}

	current = conversation.children.deliveryAt
	conversation.deliverChildCompletions()
	if !conversation.currentTurn.Running() {
		t.Fatal("the report did not start a turn")
	}
	for report := range conversation.currentTurn.Events() {
		conversation.takeTurn(report)
	}
	conversation.finish()
	if got, want := progressReports(screenOutput.String()), []string{busy, idle}; !slices.Equal(got, want) {
		t.Fatalf("the delivered report reported %q, want %q", got, want)
	}
}

func TestAStoppedChildEndsTheBusyStatusOfAnIdleParent(t *testing.T) {
	var drawn bytes.Buffer
	conversation := testConversation(t, &drawn)
	var screenOutput strings.Builder
	conversation.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	conversation.start("watch the build")
	conversation.subagentEvent(subagentrecord.StartedEvent("tame-adder", "adder", "Watch the build", subagentrecord.Origin{}))
	for report := range conversation.currentTurn.Events() {
		conversation.takeTurn(report)
	}
	conversation.finish()
	conversation.subagentEvent(subagentrecord.SentEvent("tame-adder", "carry on"))
	conversation.subagentEvent(subagentrecord.FinishedEvent("tame-adder", subagentrecord.Stopped, "", nil, &agent.Usage{}))
	if got, want := progressReports(screenOutput.String()), []string{"\x1b]9;4;3\x1b\\", "\x1b]9;4;0\x1b\\"}; !slices.Equal(got, want) {
		t.Fatalf("stopping the last child reported %q, want %q", got, want)
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
	message := completionMessage(first, func(name string) string { return " (scratch of " + name + ")" }, noAlias)
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
	message := completionMessage(due, func(string) string { return "" }, noAlias)
	want := "Subagent tame-adder interjects:\n\nIs the parser in scope?\n\n" +
		"Subagent tame-alpaca finished:\n\ntame-alpaca done: second answer"
	if message != want {
		t.Errorf("told the parent %q, want %q", message, want)
	}
	reports := completionReports(due)
	if len(reports) != 2 || !reports[0].IsMessage() || reports[0].Message != "Is the parser in scope?" || reports[1].IsMessage() {
		t.Errorf("reported %+v", reports)
	}
	if notice := reports[0].Notice(""); notice != "Subagent tame-adder interjects:\n\nIs the parser in scope?" {
		t.Errorf("drew %q", notice)
	}
}

func noAlias(string) string { return "" }

func TestAnAliasedChildIsHeadedByItsNameAndAlias(t *testing.T) {
	aliasOf := func(name string) string { return map[string]string{"tame-adder": "core"}[name] }
	due := []agent.Event{
		subagentrecord.ReceivedEvent("tame-adder", "Is the parser in scope?"),
		finished("tame-adder", "first answer"),
		finished("tame-alpaca", "second answer"),
	}
	message := completionMessage(due, func(string) string { return "" }, aliasOf)
	want := "Subagent tame-adder (core) interjects:\n\nIs the parser in scope?\n\n" +
		"Subagents tame-adder (core), tame-alpaca finished:\n\ntame-adder done: first answer\n\ntame-alpaca done: second answer"
	if message != want {
		t.Errorf("told the parent %q, want %q", message, want)
	}
	reports := completionReports(due)
	for index, want := range []string{
		"Subagent tame-adder (core) interjects:\n\nIs the parser in scope?",
		"Subagent tame-adder (core) has finished:\n\nfirst answer",
		"Subagent tame-alpaca has finished:\n\nsecond answer",
	} {
		if notice := reports[index].Notice(aliasOf(reports[index].Name)); notice != want {
			t.Errorf("drew %q, want %q", notice, want)
		}
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

func TestReloadingConfigChangesTheSubagentLimitAndModelsForTheNextStart(t *testing.T) {
	const listedHaiku = "[[subagent.model]]\nmodel = \"anthropic/haiku\"\n"
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[subagent]\nconcurrency = 2\n"+listedHaiku)

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)
	choice := model.Choice{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}}
	catalogueOf := func(settings config.Subagent, defaults model.Defaults) subagents.Catalogue {
		return childCatalogueOf(choice, settings, defaults, []model.Choice{choice})
	}
	initial := catalogueOf(configFrom(t, "[subagent]\nconcurrency = 2\n"+listedHaiku).Subagent, model.Defaults{})
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
	self.children = childState{manager: manager, catalogueOf: catalogueOf}

	writeLiveConfig(t, path, "[subagent]\nconcurrency = 7\n"+listedHaiku)
	settleLiveConfig(t, self)
	if got := manager.Concurrency(); got != 7 {
		t.Errorf("the limit after a reload is %d, want 7", got)
	}
	listedHaikuModels := manager.Models()
	if !strings.Contains(listedHaikuModels, "- anthropic/claude-haiku-5-5@high") {
		t.Errorf("the listed models after a reload are %q, want haiku", listedHaikuModels)
	}

	writeLiveConfig(t, path, "[subagent]\nconcurrency = 0\n"+listedHaiku)
	settleLiveConfig(t, self)
	if got := manager.Concurrency(); got != 7 || manager.Models() != listedHaikuModels {
		t.Errorf("an invalid limit replaced the one standing: %d, %q", got, manager.Models())
	}
	if message := self.feedback.Message(); message.Status != agent.ErrorStatus || !strings.Contains(message.Text, "subagent.concurrency") {
		t.Errorf("an invalid limit said %+v", message)
	}

	writeLiveConfig(t, path, "[subagent]\nconcurrency = 7\n"+listedHaiku+"availability = \"everyone\"\n")
	settleLiveConfig(t, self)
	if got := manager.Models(); got != listedHaikuModels {
		t.Errorf("an invalid availability replaced the models standing with %q", got)
	}
	if message := self.feedback.Message(); message.Status != agent.ErrorStatus || !strings.Contains(message.Text, "subagent.model[1].availability") {
		t.Errorf("an invalid availability said %+v", message)
	}

	writeLiveConfig(t, path, "[subagent]\nconcurrency = 7\n")
	settleLiveConfig(t, self)
	if got := manager.Models(); strings.Contains(got, "claude-haiku") {
		t.Errorf("a config listing nothing still offers %q", got)
	}
}

func TestAnEmptySubagentCatalogueKeepsItsConcurrency(t *testing.T) {
	catalogue := childCatalogueOf(model.Choice{}, config.Subagent{Concurrency: 4, IsFree: true}, model.Defaults{}, nil)
	if catalogue.Concurrency != 4 || len(catalogue.Models) != 0 {
		t.Fatalf("an empty catalogue came back as %+v", catalogue)
	}
}

func offeredTitles(catalogue subagents.Catalogue) []string {
	titles := make([]string, 0, len(catalogue.Models))
	for _, offered := range catalogue.Models {
		if offered.Label == "" {
			titles = append(titles, offered.Selection.String())
			continue
		}
		members := make([]string, 0, len(offered.Members))
		for _, member := range offered.Members {
			members = append(members, member.Selection.String())
		}
		titles = append(titles, offered.Label+" = "+strings.Join(members, " | "))
	}
	return titles
}

func TestAnOllamaSessionStartsSubagentsOnlyOnOllamaModels(t *testing.T) {
	local := model.Choice{Provider: model.OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k", EffortLevels: []string{"none"}}
	other := model.Choice{Provider: model.OllamaProvider, ID: "muse-glimmer-128k:latest", EffortLevels: []string{"none"}}
	hosted := model.Choice{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}}
	free := model.Choice{Provider: model.OpencodeGoProvider, ID: "cat-jam-free"}
	choices := []model.Choice{local, other, hosted, free}
	settings := configFrom(t, `
		[subagent]
		free = true

		[[subagent.model]]
		model = "ollama/qwen"
		availability = "all"

		[[subagent.model]]
		model = "anthropic/haiku"
		availability = "all"

		[[subagent.model]]
		label = "mixed"
		round_robin = ["ollama/qwen", "anthropic/haiku", "opencode-go/cat-jam-free"]
		availability = "all"
	`).Subagent
	for _, test := range []struct {
		parent model.Choice
		want   []string
	}{
		{other, []string{"ollama/qwen3.8:27b-mtp-q8_0-256k@none", "mixed = ollama/qwen3.8:27b-mtp-q8_0-256k@none"}},
		{hosted, []string{"anthropic/claude-haiku-5-5@high", "mixed = anthropic/claude-haiku-5-5@high | opencode-go/cat-jam-free", "opencode-go/cat-jam-free"}},
	} {
		if got := offeredTitles(childCatalogueOf(test.parent, settings, model.Defaults{}, choices)); !slices.Equal(got, test.want) {
			t.Errorf("a session on %s offers %v, want %v", test.parent.Provider, got, test.want)
		}
	}
}

func TestALabelledEntryCarriesItsDescriptionAndSystemPromptToEveryMember(t *testing.T) {
	parent := model.Choice{Provider: model.AnthropicProvider, ID: "claude-opus-5-5"}
	settings := configFrom(t, `
		[subagent]
		free = false

		[[subagent.model]]
		label = "explorer"
		round_robin = ["anthropic/haiku@high", "anthropic/fable@low"]
		description = "reading code"
		system_prompt = "You explore."

		[[subagent.model]]
		model = "anthropic/fable@high"
		description = "deep reviews"
		system_prompt = "You review."
	`).Subagent
	catalogue := childCatalogueOf(parent, settings, model.Defaults{}, subagentCatalogueChoices)
	if len(catalogue.Models) != 2 {
		t.Fatalf("offered %v", offeredTitles(catalogue))
	}
	explorer, reviewer := catalogue.Models[0], catalogue.Models[1]
	if explorer.Label != "explorer" || explorer.Description != "reading code" || explorer.SystemPrompt != "You explore." || len(explorer.Members) != 2 {
		t.Errorf("the explorer is %+v", explorer)
	}
	for _, member := range explorer.Members {
		if member.Label != "" || member.SystemPrompt != "" || member.Choice.ID == "" {
			t.Errorf("a member carries its own identity or lacks a model: %+v", member)
		}
	}
	if reviewer.Label != "" || reviewer.Selection.String() != "anthropic/claude-fable-5-1@high" || reviewer.SystemPrompt != "You review." || reviewer.Description != "deep reviews" {
		t.Errorf("the reviewer is %+v", reviewer)
	}
}

func TestASessionCanStartChildrenOnlyWithAnEligibleModel(t *testing.T) {
	local := model.Choice{Provider: model.OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k", EffortLevels: []string{"none"}}
	hosted := model.Choice{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}}
	free := model.Choice{Provider: model.OpencodeGoProvider, ID: "cat-jam-free"}
	listed := config.Subagent{Models: []config.SubagentModel{
		{ModelChoice: config.ModelChoice{Model: "ollama/qwen"}},
		{ModelChoice: config.ModelChoice{Model: "anthropic/haiku"}},
	}}
	for _, test := range []struct {
		name     string
		parent   model.Choice
		settings config.Subagent
		choices  []model.Choice
		want     bool
	}{
		{"nothing listed or free", hosted, config.Subagent{IsFree: true}, []model.Choice{local, hosted}, false},
		{"a listed model of the parent's provider", hosted, listed, []model.Choice{local, hosted}, true},
		{"an ollama session with a listed local model", local, listed, []model.Choice{local, hosted}, true},
		{"a free model with nothing listed", hosted, config.Subagent{IsFree: true}, []model.Choice{hosted, free}, true},
		{"a free model while free models are off", hosted, config.Subagent{}, []model.Choice{hosted, free}, false},
		{"an ollama session beside a free model", local, config.Subagent{IsFree: true}, []model.Choice{local, free}, false},
		{"a hosted session beside another provider's listed model", model.Choice{Provider: model.CodexProvider}, listed, []model.Choice{local, hosted}, false},
	} {
		if got := canStartChildren(test.parent, config.Config{Subagent: test.settings}, test.choices); got != test.want {
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
		{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"medium", "high"}},
		{Provider: model.OpencodeGoProvider, ID: "deepseek-v4.1-flash", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "gpt-6-luna", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "longcat-2.5-preview-free"},
		{Provider: model.OpencodeGoProvider, ID: "step-5-preview-free", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "cat-jam-free", EffortLevels: []string{"high"}},
		{Provider: model.OllamaProvider, ID: "muse-glimmer-128k:latest", EffortLevels: []string{"high"}},
		{Provider: model.OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k", EffortLevels: []string{"high"}},
	}
	settings := configFrom(t, `
		[subagent]
		free = true

		[[subagent.model]]
		model = "haiku@high"
		description = "exploration work"

		[[subagent.model]]
		model = "haiku@medium"
		description = "exploration work"

		[[subagent.model]]
		model = "luna"

		[[subagent.model]]
		model = "deepseek-v4.1-flash@high"

		[[subagent.model]]
		model = "qwen"
	`).Subagent
	passes := map[string]func() string{}
	for _, provider := range model.ProviderNames() {
		passes[provider] = func() string {
			catalogue := childCatalogueOf(model.Choice{Provider: provider}, settings, model.Defaults{}, choices)
			manager, err := subagents.New(subagents.Options{Directory: t.TempDir(), Scratch: t.TempDir(), Models: catalogue.Models})
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
	settings := config.Subagent{Models: []config.SubagentModel{
		{ModelChoice: config.ModelChoice{Model: "codex/luna"}},
		{ModelChoice: config.ModelChoice{RoundRobin: config.RoundRobin{"codex/luna"}}, Label: "explorer"},
	}}
	catalogue := childCatalogueOf(choice, settings, model.Defaults{IsFast: true}, []model.Choice{choice})
	if len(catalogue.Models) != 2 || catalogue.Models[0].Selection.IsFast || catalogue.Models[1].Members[0].Selection.IsFast {
		t.Errorf("fast defaults reached the subagent catalogue: %+v", catalogue)
	}
}

var subagentCatalogueChoices = []model.Choice{
	{Provider: model.AnthropicProvider, ID: "claude-opus-5-5", EffortLevels: []string{"low", "medium", "high", "max"}},
	{Provider: model.AnthropicProvider, ID: "claude-fable-5-1", EffortLevels: []string{"low", "medium", "high"}},
	{Provider: model.AnthropicProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"low", "medium", "high"}},
	{Provider: model.CodexProvider, ID: "gpt-6.1-sol", EffortLevels: []string{"low", "medium", "high"}},
	{Provider: model.CodexProvider, ID: "gpt-6-luna", EffortLevels: []string{"low", "medium", "high"}},
	{Provider: model.OpencodeGoProvider, ID: "glm-5.3"},
	{Provider: model.OpencodeGoProvider, ID: "deepseek-v4.1-flash", EffortLevels: []string{"high"}},
	{Provider: model.OpencodeGoProvider, ID: "claude-haiku-5-5", EffortLevels: []string{"high"}},
	{Provider: model.OpencodeGoProvider, ID: "step-5-preview-free", EffortLevels: []string{"low", "high"}},
	{Provider: model.OpencodeGoProvider, ID: "cat-jam-free"},
	{Provider: model.OllamaProvider, ID: "qwen3.8:27b", EffortLevels: []string{"none"}},
	{Provider: model.OllamaProvider, ID: "muse-glimmer:latest", EffortLevels: []string{"none"}},
}

func drawnSubagentCatalogue(t *testing.T, parent string, body string) string {
	t.Helper()

	provider, id, _ := strings.Cut(parent, "/")
	parentChoice := model.Choice{Provider: provider, ID: id}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, fmt.Appendf(nil, "version = %d\n%s", config.Format, body), 0o600); err != nil {
		t.Fatal(err)
	}
	var drawn strings.Builder
	fmt.Fprintf(&drawn, "parent: %s\n", parent)
	for line := range strings.Lines(body) {
		if strings.TrimSpace(line) == "" {
			drawn.WriteString(">\n")
			continue
		}
		fmt.Fprintf(&drawn, "> %s", line)
	}
	drawn.WriteString("\n")
	settings, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(&drawn, "refused: %s\n", strings.ReplaceAll(err.Error(), path, "config.toml"))
		return drawn.String()
	}
	for _, unknown := range settings.UnknownSettings() {
		fmt.Fprintf(&drawn, "warned: %s\n", strings.ReplaceAll(unknown, path, "config.toml"))
	}
	catalogue := childCatalogueOf(parentChoice, settings.Subagent, settings.Defaults.ForSelections(), subagentCatalogueChoices)
	manager, err := subagents.New(subagents.Options{Directory: t.TempDir(), Scratch: t.TempDir(), Models: catalogue.Models})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	fmt.Fprintf(&drawn, "%s\n", manager.Models())
	for _, offered := range catalogue.Models {
		if offered.Label == "" && offered.SystemPrompt == "" {
			continue
		}
		title := offered.Label
		if title == "" {
			title = offered.Selection.String()
		}
		fmt.Fprintf(&drawn, "\nhidden from the parent, for %s:\n", title)
		for _, member := range offered.Members {
			fmt.Fprintf(&drawn, "  member: %s\n", member.Selection)
		}
		if offered.SystemPrompt != "" {
			fmt.Fprintf(&drawn, "  system prompt: %s\n", offered.SystemPrompt)
		}
	}
	return drawn.String()
}

func TestGoldenSubagentModelCatalogue(t *testing.T) {
	const anthropic = "anthropic/claude-opus-5-5"
	const codex = "codex/gpt-6.1-sol"
	const opencodeGo = "opencode-go/glm-5.3"
	const ollama = "ollama/muse-glimmer:latest"
	cases := []struct {
		name   string
		parent string
		body   string
	}{
		{"01 the defaults offer nothing", anthropic, ""},
		{"02 free models switched on are offered to every hosted parent", anthropic, "[subagent]\nfree = true\n"},
		{"03 a model carries its effort", anthropic, "[[subagent.model]]\nmodel = \"anthropic/haiku@medium\"\n"},
		{"04 a model without an effort takes the automatic effort", anthropic, "[[subagent.model]]\nmodel = \"anthropic/haiku\"\n"},
		{"05 the automatic effort follows the default effort", anthropic, "[default]\neffort = \"max\"\n\n[[subagent.model]]\nmodel = \"opus\"\n"},
		{"06 a description follows its line", anthropic, "[[subagent.model]]\nmodel = \"anthropic/haiku@high\"\ndescription = \"exploration work\"\n\n[[subagent.model]]\nmodel = \"anthropic/haiku@low\"\ndescription = \"quick checks\"\n"},
		{"07 entries keep config order", anthropic, "[[subagent.model]]\nmodel = \"haiku\"\n\n[[subagent.model]]\nmodel = \"fable\"\n\n[[subagent.model]]\nmodel = \"opus@low\"\n"},
		{"08 same provider is the default availability", codex, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"anthropic/haiku\"\n\n[[subagent.model]]\nmodel = \"codex/luna\"\n"},
		{"09 availability all reaches another provider", codex, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"anthropic/haiku\"\navailability = \"all\"\n"},
		{"10 a model matching several providers is passed over", codex, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"haiku\"\navailability = \"all\"\n\n[[subagent.model]]\nmodel = \"opencode-go/haiku\"\navailability = \"all\"\n"},
		{"11 same provider settles which provider a name means", opencodeGo, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"haiku\"\n"},
		{"12 an effort the model lacks is passed over", anthropic, "[[subagent.model]]\nmodel = \"fable@max\"\n\n[[subagent.model]]\nmodel = \"fable@high\"\n"},
		{"13 a model with no effort levels takes no effort", opencodeGo, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"glm@high\"\n\n[[subagent.model]]\nmodel = \"glm\"\n"},
		{"14 an entry naming a free model replaces its default line", anthropic, "[subagent]\nfree = true\n\n[[subagent.model]]\nmodel = \"step@low\"\ndescription = \"quick lookups\"\n"},
		{"15 an entry keeps a free model while free models are off", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"step\"\n"},
		{"16 a duplicate selection keeps the first entry", anthropic, "[[subagent.model]]\nmodel = \"haiku@high\"\ndescription = \"exploration work\"\n\n[[subagent.model]]\nmodel = \"claude-haiku-5-5@high\"\ndescription = \"anything\"\n"},
		{"17 a model matching nothing is passed over", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"nonexistent\"\n\n[[subagent.model]]\nmodel = \"haiku\"\n"},
		{"18 a local parent sees only listed local models", ollama, "[[subagent.model]]\nmodel = \"qwen\"\n\n[[subagent.model]]\nmodel = \"anthropic/haiku\"\navailability = \"all\"\n"},
		{"19 a local parent with nothing listed has nothing", ollama, ""},
		{"20 a hosted parent never sees a local model", codex, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"ollama/qwen\"\navailability = \"all\"\n"},
		{"21 a label hides its model", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\nmodel = \"anthropic/haiku@high\"\ndescription = \"reading code and finding where things live\"\nsystem_prompt = \"You are a careful explorer. Report paths and line numbers, never guesses.\"\n"},
		{"22 a system prompt can sit behind a visible model", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"anthropic/fable@high\"\nsystem_prompt = \"You review finished changes.\"\n"},
		{"23 a labelled rotation lists its label once", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\nround_robin = [\"anthropic/haiku@high\", \"codex/luna@medium\", \"opencode-go/deepseek-v4.1-flash\"]\navailability = \"all\"\ndescription = \"reading code\"\n"},
		{"24 same provider drops other providers from a rotation", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\nround_robin = [\"anthropic/haiku@high\", \"codex/luna@medium\", \"opencode-go/step-5-preview-free@low\"]\n"},
		{"25 a rotation keeps repeated members as weights", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\nround_robin = [\"anthropic/haiku@high\", \"anthropic/haiku@high\", \"anthropic/fable@high\"]\n"},
		{"26 a rotation with no member left drops its label", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\nround_robin = [\"codex/luna@medium\", \"anthropic/nonexistent\"]\n\n[[subagent.model]]\nmodel = \"haiku\"\n"},
		{"27 a label and a bare line can share a model", anthropic, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\nmodel = \"anthropic/haiku@high\"\n\n[[subagent.model]]\nmodel = \"anthropic/haiku@high\"\n"},
		{"28 a labelled free model keeps the default free line", anthropic, "[subagent]\nfree = true\n\n[[subagent.model]]\nlabel = \"scout\"\nmodel = \"step@low\"\n"},
		{"29 a local parent takes a labelled local rotation", ollama, "[[subagent.model]]\nlabel = \"local\"\nround_robin = [\"qwen\", \"muse\", \"anthropic/haiku\"]\navailability = \"all\"\n"},
		{"30 an entry without a model or rotation is refused", anthropic, "[[subagent.model]]\nlabel = \"explorer\"\n"},
		{"31 a model beside a rotation is refused", anthropic, "[[subagent.model]]\nlabel = \"explorer\"\nmodel = \"anthropic/haiku\"\nround_robin = [\"anthropic/fable\"]\n"},
		{"32 a rotation without a label is refused", anthropic, "[[subagent.model]]\nround_robin = [\"anthropic/haiku\", \"anthropic/fable\"]\n"},
		{"33 an empty rotation is refused", anthropic, "[[subagent.model]]\nlabel = \"explorer\"\nround_robin = []\n"},
		{"34 a label outside lowercase letters, digits, and hyphens is refused", anthropic, "[[subagent.model]]\nlabel = \"Explorer\"\nmodel = \"anthropic/haiku\"\n"},
		{"35 a repeated label is refused", anthropic, "[[subagent.model]]\nlabel = \"explorer\"\nmodel = \"anthropic/haiku\"\n\n[[subagent.model]]\nlabel = \"explorer\"\nmodel = \"anthropic/fable\"\n"},
		{"36 an unknown availability is refused", anthropic, "[[subagent.model]]\nmodel = \"anthropic/haiku\"\navailability = \"everyone\"\n"},
		{"37 a mistyped setting is warned about", anthropic, "[[subagent.model]]\nmodel = \"anthropic/haiku\"\ndescriptions = \"exploration work\"\n"},
		{"38 the earlier name and effort keys are refused", anthropic, "[[subagent.model]]\nname = \"haiku\"\neffort = \"high\"\n"},
		{"39 the removed single model form is refused", anthropic, "[subagent]\nmodel = \"anthropic/claude-haiku-5-5\"\n"},
	}
	passes := map[string]func() string{}
	for _, test := range cases {
		passes[test.name] = func() string {
			return drawnSubagentCatalogue(t, test.parent, test.body)
		}
	}
	compareWithGolden(t, "subagent-model-catalogue", ".txt", passes)
}

func TestGoldenSubagentRotationsTakeTurns(t *testing.T) {
	parent := model.Choice{Provider: model.AnthropicProvider, ID: "claude-opus-5-5"}
	cases := []struct {
		name   string
		body   string
		reload string
	}{
		{"1 each start takes the next member", "round_robin = [\"anthropic/haiku@high\", \"codex/luna@medium\", \"opencode-go/deepseek-v4.1-flash\"]\navailability = \"all\"\n", ""},
		{"2 a repeated member takes more turns", "round_robin = [\"anthropic/haiku@high\", \"anthropic/haiku@high\", \"anthropic/fable@high\"]\n", ""},
		{"3 a single model never moves", "model = \"anthropic/haiku@high\"\n", ""},
		{
			"4 the turn carries on after a reload adds a member",
			"round_robin = [\"anthropic/haiku@high\", \"anthropic/fable@high\"]\n",
			"round_robin = [\"anthropic/haiku@high\", \"anthropic/fable@high\", \"anthropic/opus@low\"]\n",
		},
		{
			"5 a reload that drops the last member starts afresh",
			"round_robin = [\"anthropic/haiku@high\", \"anthropic/fable@high\"]\n",
			"round_robin = [\"anthropic/opus@low\", \"anthropic/haiku@high\"]\n",
		},
	}
	passes := map[string]func() string{}
	for _, test := range cases {
		passes[test.name] = func() string {
			directory := t.TempDir()
			reserve := reserveChildMember(directory)
			var drawn strings.Builder
			turns := func(body string, count int) {
				settings := configFrom(t, "[subagent]\nfree = false\n\n[[subagent.model]]\nlabel = \"explorer\"\n"+body).Subagent
				offered := childCatalogueOf(parent, settings, model.Defaults{}, subagentCatalogueChoices).Models[0]
				for line := range strings.Lines(body) {
					fmt.Fprintf(&drawn, "> %s", line)
				}
				for turn := 1; turn <= count; turn++ {
					member, err := reserve(offered.Label, offered.Members)
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintf(&drawn, "start %d: %s\n", turn, member.Selection)
				}
			}
			turns(test.body, 4)
			if test.reload != "" {
				drawn.WriteString("--- reload ---\n")
				turns(test.reload, 4)
			}
			return drawn.String()
		}
	}
	compareWithGolden(t, "subagent-model-rotation", ".txt", passes)
}

func TestGoldenSubagentModelsReloadLive(t *testing.T) {
	const explorerAndFable = `
		[subagent]
		concurrency = 2
		free = false

		[[subagent.model]]
		label = "explorer"
		round_robin = ["anthropic/haiku@high", "anthropic/haiku@low"]
		description = "exploration work"

		[[subagent.model]]
		model = "anthropic/fable"
	`
	stages := []struct {
		name  string
		body  string
		probe string
	}{
		{"1 starts with haiku and free models listed", "[subagent]\nfree = true\n\n[[subagent.model]]\nmodel = \"anthropic/haiku@high\"\n", ""},
		{"2 a reload adds a labelled rotation and fable, turns free models off, and lowers the limit", explorerAndFable, ""},
		{"3 an invalid availability is refused and the list stands", explorerAndFable + "availability = \"everyone\"\n", ""},
		{"4 a mistyped setting is warned about", explorerAndFable + "descriptions = \"deep reviews\"\n", ""},
		{"5 a corrected setting applies", explorerAndFable + "description = \"deep reviews\"\n", ""},
		{"6 removing the explorer refuses a stale explorer selection", "[subagent]\nfree = false\n\n[[subagent.model]]\nmodel = \"anthropic/fable\"\n", "explorer"},
	}
	passes := map[string]func() string{}
	for index, stage := range stages {
		passes[stage.name] = func() string {
			path := filepath.Join(t.TempDir(), "config.toml")
			writeLiveConfig(t, path, stages[0].body)
			self := testConversation(t, &bytes.Buffer{})
			prepareLiveConfig(t, self, path)
			parent := model.Choice{Provider: model.AnthropicProvider, ID: "claude-opus-5-5"}
			catalogueOf := func(settings config.Subagent, defaults model.Defaults) subagents.Catalogue {
				return childCatalogueOf(parent, settings, defaults, subagentCatalogueChoices)
			}
			initial := configFrom(t, stages[0].body)
			catalogue := catalogueOf(initial.Subagent, initial.Defaults.ForSelections())
			manager, err := subagents.New(subagents.Options{
				Directory:   filepath.Join(t.TempDir(), "subagents"),
				Scratch:     t.TempDir(),
				Parent:      "tame-impala",
				Models:      catalogue.Models,
				Concurrency: catalogue.Concurrency,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			self.children = childState{manager: manager, catalogueOf: catalogueOf}

			for _, earlier := range stages[1 : index+1] {
				writeLiveConfig(t, path, earlier.body)
				settleLiveConfig(t, self)
			}

			var drawn strings.Builder
			self.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			if !self.feedback.IsEmpty() {
				message := self.feedback.Message()
				message.Text = strings.ReplaceAll(message.Text, path, "config.toml")
				self.feedback.Show(feedback.Config, message, self.getNow())
			}
			self.show(edit.NewInput(nil))
			var result strings.Builder
			result.WriteString(shown(t, drawn.String(), replayColumns))
			fmt.Fprintf(&result, "\n--- action=models, at most %d at once ---\n%s\n", manager.Concurrency(), manager.Models())
			if stage.probe != "" {
				_, err := manager.Start(t.Context(), "", []subagent.Task{{Model: stage.probe, Prompt: "Review the parser", Intent: "Reviewing the parser"}})
				fmt.Fprintf(&result, "--- start %s ---\n%v\n", stage.probe, err)
			}
			return result.String()
		}
	}
	compareWithGolden(t, "subagent-model-reload", ".screen", passes)
}
