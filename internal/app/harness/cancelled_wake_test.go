package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/notification"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
)

func wakingNotices() map[string]func(*App) {
	return map[string]func(*App){
		"task": func(self *App) {
			self.tasksDoWake = true
			self.taskEnded(endedTaskConclusion())
		},
		"subagent completion": func(self *App) {
			self.subagentEvent(finished("tame-adder", "The tests passed."))
			self.children.deliveryAt = self.getNow()
			self.deliverChildCompletions()
		},
		"subagent message": func(self *App) {
			self.subagentEvent(subagentrecord.ReceivedEvent("tame-adder", "The tests are running."))
			self.children.deliveryAt = self.getNow()
			self.deliverChildCompletions()
		},
		"experimental host command": func(self *App) {
			self.experimental = experimental.New(map[string]any{
				string(experimental.CommandStartsTurn): true,
			})
			self.hostCommandRan(hostCommandRun())
		},
	}
}

func TestCancellationKeepsWakingNoticesPendingUntilTheUserStartsATurn(t *testing.T) {
	for name, deliver := range wakingNotices() {
		for _, cause := range []interrupt.Cause{interrupt.Escape, interrupt.ControlD} {
			for _, arrivesBeforeFinish := range []bool{true, false} {
				stage := "after finish"
				if arrivesBeforeFinish {
					stage = "before finish"
				}
				t.Run(name+"/"+string(cause)+"/"+stage, func(t *testing.T) {
					var drawn bytes.Buffer
					self := testConversation(t, &drawn)
					provider := &messageCaptureProvider{}
					self.agent = agent.New("", provider, nil)
					self.start("watch the background work")
					for report := range self.currentTurn.Events() {
						self.takeTurn(report)
					}
					self.cancelTurn(cause)
					if arrivesBeforeFinish {
						deliver(self)
					}
					self.finish()
					messagesBefore := len(provider.messages)
					if !arrivesBeforeFinish {
						deliver(self)
					}
					if self.currentTurn.Running() {
						t.Fatal("a background notice restarted the cancelled turn")
					}
					if len(self.pendingNotices.items) != 1 || self.pendingNotices.block == nil {
						t.Fatalf("want one visible pending notice, got %+v", self.pendingNotices)
					}
					if len(provider.messages) != messagesBefore {
						t.Fatal("the pending notice reached the provider before a user turn")
					}

					history := edit.NewHistory("", historyLimit)
					self.continueOrFlush(edit.NewInput(history), history)
					if !self.currentTurn.Running() || self.currentTurn.Cancelled() || len(self.pendingNotices.items) != 0 {
						t.Fatal("double-enter did not submit the pending notice in a fresh turn")
					}
					for report := range self.currentTurn.Events() {
						self.takeTurn(report)
					}
					self.finish()

					deliver(self)
					if !self.currentTurn.Running() {
						t.Fatal("a user-started turn did not restore automatic wake-ups")
					}
					for report := range self.currentTurn.Events() {
						self.takeTurn(report)
					}
					self.finish()
				})
			}
		}
	}
}

func TestCancelledWakeNotifiesOnceWhileUnfocusedAndResetsOnAUserTurn(t *testing.T) {
	var drawn bytes.Buffer
	self := testConversation(t, &drawn)
	self.terminal = terminal.New(&drawn, work.At("/workspace/project"))
	self.terminal.ObserveFocus(key.FocusIn)
	announcements := 0
	self.onPendingWake = func() { announcements++ }
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.cancelTurn(interrupt.Escape)
	self.currentTurn.Interjections().Finish()
	self.finish()
	wakingNotices()["task"](self)
	if announcements != 0 {
		t.Fatal("a pending result notified the focused terminal")
	}
	self.terminal.ObserveFocus(key.FocusOut)
	wakingNotices()["subagent completion"](self)
	wakingNotices()["subagent message"](self)
	self.wake()
	if announcements != 1 {
		t.Fatalf("got %d notifications, want one for all pending results", announcements)
	}
	self.start("resume the work")
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.cancelTurn(interrupt.ControlD)
	wakingNotices()["task"](self)
	if announcements != 1 {
		t.Fatal("notified before the cancelled turn finished")
	}
	self.finish()
	if announcements != 2 {
		t.Fatalf("got %d notifications, want another after a fresh cancellation", announcements)
	}
}

func TestACancelledTurnKeepsATaskPendingAndNotifiesThroughTheBinary(t *testing.T) {
	fakeKitten(t)
	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{
				Name: "bash",
				Arguments: encodedArguments(map[string]any{
					"command":    "while ! test -e release-task; do sleep 0.01; done; printf 'background work finished\\n'",
					"background": true,
					"task_name":  "build",
					"intent":     "Waiting for the background work release",
				}),
			}}},
			{Say: "Waiting for background work. " + strings.Repeat("Still waiting. ", 200), Pace: sim.Duration{Duration: 20 * time.Millisecond}},
			{Say: "Resumed only when asked."},
		},
	})
	rig, _ := newEndpointRig(t, endpoint)
	rig.environment = append(rig.environment, "KITTY_WINDOW_ID=1")
	session := rig.start("--yolo", "-m", "opencode-go/fake", "start the background work")
	if !session.WaitForCount("Waiting for background work.", 1, interactiveDeadline) {
		t.Fatal("the model never started its answer")
	}
	session.typeText(pressEscape)
	session.waitFor("Turn stopped because the user pressed escape.")
	session.typeText("\x1b[Omy unfinished message")
	session.waitFor("my unfinished message")
	if err := os.WriteFile(filepath.Join(rig.workspace, "release-task"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	session.waitFor("Background results are waiting. The turn remains cancelled.")
	session.requireShown("double-enter to send")
	session.requireShown("my unfinished message")
	if got := len(endpoint.Requests()); got != 2 {
		t.Fatalf("got %d provider requests before resuming, want two", got)
	}
	session.typeText("\x1b[I" + clearInput + "resume now" + pressEnter)
	session.waitFor("Resumed only when asked.")
	session.quit()
	requests := endpoint.Requests()
	if len(requests) != 3 {
		t.Fatalf("got %d provider requests, want three after the user's message", len(requests))
	}
}

func TestGoldenBackgroundNoticesStayPendingAfterCancellation(t *testing.T) {
	fakeKitten(t)
	passes := map[string]func() string{}
	for name, deliver := range wakingNotices() {
		passes[name] = func() string {
			var drawn bytes.Buffer
			self := testConversation(t, &drawn)
			self.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			self.recorder = namedGoldenRecorder(t)
			self.onPendingWake = func() {
				if err := notification.SendPendingWake(t.Context(), self.screen.WriteEscape, self.terminal.IsFocused, work.At("/workspace/project")); err != nil {
					t.Fatal(err)
				}
			}
			self.settleAccess()
			self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
			self.cancelTurn(interrupt.Escape)
			self.currentTurn.Interjections().Finish()
			deliver(self)
			self.finish()
			if self.currentTurn.Running() {
				t.Fatal("a background notice restarted the cancelled turn")
			}
			history := edit.NewHistory("", historyLimit)
			self.show(edit.NewInput(history))
			self.screen.End()
			if !strings.Contains(drawn.String(), "⏳") {
				t.Fatal("the background notice was not drawn as pending")
			}
			return drawn.String()
		}
	}
	compareWithGolden(t, "cancelled-wake", ".ansi", passes)
	compareWithGolden(t, "cancelled-wake", ".screen", shownPasses(t, passes))
}
