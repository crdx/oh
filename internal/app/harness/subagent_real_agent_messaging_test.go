package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/interaction"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/toolbox/messageparent"
)

func TestInProcessASubagentMessagesItsParentMidRunWithRealAgentStates(t *testing.T) {
	fixture := newRealSubagentFixture(t, messagingResponder(), nil)
	if err := os.WriteFile(filepath.Join(fixture.workspace.GetDir(), "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	controller, terminal := ptytest.OpenSized(t, interactiveColumns, interactiveRows)
	restoreTTY, err := tty.Raw(terminal, terminal)
	if err != nil {
		t.Fatal(err)
	}
	defer restoreTTY()
	transcript := ptytest.Record(controller)
	screen := output.NewTerminalOfSize(terminal, interactiveColumns, interactiveRows)
	screen.BeginEditing()
	defer screen.Release(true)
	assistant := agent.New(fixture.system, fixture.provider, fixture.parentTools())
	assistant.TakeRetryWaitsAtOnce()
	inputLine := edit.NewInput(nil)
	app := &App{
		agent:            assistant,
		screen:           screen,
		recorder:         record.New(fixture.parentLog),
		workspace:        fixture.workspace,
		sessionDirectory: session.Dir(fixture.sessions, fixture.parentName),
		display:          fixtureDisplay(),
		experimental:     experimental.New(map[string]any{}),
		mode:             fixture.mode,
		children: childState{
			manager:   fixture.manager,
			directory: session.ChildrenDir(fixture.sessions, fixture.parentName),
		},
		now: time.Now,
	}
	app.start(messagingPrompt)
	keyboard := interaction.NewKeyboard(terminal)
	loopDone := make(chan struct{})
	go func() {
		interaction.Run(keyboard, app.nextRefresh, interaction.Handler{
			GetTurnEvents: func() <-chan TurnEvent { return app.currentTurn.Events() },
			OnKey:         func(key.Key) bool { return false },
			OnTurn:        app.takeTurn,
			OnTurnFinished: func() bool {
				app.finish()
				return true
			},
			SubagentEvents:   app.subagentEvents(),
			SubagentActivity: app.subagentActivity(),
			OnSubagentEvent:  app.subagentEvent,
			OnDraw:           func() { app.drawAfterEvent(inputLine) },
		})
		close(loopDone)
	}()

	for _, want := range []string{parentStartedAnswer, childQuestion, parentRepliedAnswer, parentFinalAnswer} {
		if !transcript.WaitForCount(want, 1, interactiveDeadline) {
			t.Fatalf("%q was never drawn:\n%s", want, transcript.String())
		}
	}
	for _, want := range []string{"interjects:", "has finished:", childQuotedAnswer} {
		if !transcript.WaitForCount(want, 1, interactiveDeadline) {
			t.Fatalf("%q was never drawn:\n%s", want, transcript.String())
		}
	}
	if _, err := controller.WriteString("\x04"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("interaction loop did not stop:\n%s", transcript.String())
	}
	if err := fixture.parentLog.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := store.Read(fixture.sessions, fixture.parentName)
	if err != nil {
		t.Fatal(err)
	}
	child := onlyRealSubagentFamily(t, fixture)
	if !slices.Contains(child.Meta.Tools, messageparent.Name) {
		t.Errorf("%s was offered %v, without %s", child.Name, child.Meta.Tools, messageparent.Name)
	}
	results := childEventsOf(child, agent.ToolCallResultEvent)
	if len(results) != 2 || results[0].Name != messageparent.Name || results[0].Text != messageparent.Result {
		t.Errorf("%s's calls came back %+v, want its message sent", child.Name, results)
	}
	received := parentFacts(stored.Events, child.Name, subagentrecord.Received)
	if len(received) != 1 || received[0].Text != childQuestion {
		t.Errorf("the parent recorded %+v, want the child question once", received)
	}
	deliveries := slices.DeleteFunc(slices.Clone(stored.Events), func(event agent.Event) bool {
		return event.Kind != subagentrecord.ReportsDelivered
	})
	if len(deliveries) != 2 || !strings.Contains(deliveries[0].Text, childQuestion) || !strings.Contains(deliveries[1].Text, childQuotedAnswer) {
		t.Errorf("delivered %+v, want the question and then the answer", deliveries)
	}
	for _, request := range fixture.endpoint.Requests() {
		isChild := strings.Contains(request.Instructions, childIdentity)
		if isChild != slices.Contains(request.Tools, messageparent.Name) {
			t.Errorf("a request offered %v, want %s for the child alone", request.Tools, messageparent.Name)
		}
	}
}

func fixtureDisplay() displayState {
	return displayState{modelName: realSubagentChoice.ID}
}

func onlyRealSubagentFamily(t *testing.T, fixture *realSubagentFixture) *store.Session {
	t.Helper()
	for _, snapshot := range fixture.manager.ListSnapshots() {
		child, err := store.Read(session.ChildrenDir(fixture.sessions, fixture.parentName), snapshot.Name)
		if err == nil {
			return child
		}
	}
	t.Fatal("no real child journal was stored")
	return nil
}
