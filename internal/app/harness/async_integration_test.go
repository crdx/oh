package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestABackgroundToolStateIsAppliedAndCanBeRestoredFromItsEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var screenOutput bytes.Buffer
		self := testConversation(t, &screenOutput)
		self.tasks = tasks.New(t.Context())
		defer self.tasks.Close()
		state := json.RawMessage(`{"digest":"changed"}`)
		var restored json.RawMessage
		subject := tool.Implement(
			tool.Definition{Name: "remember"},
			func(struct{}) tool.CallRendering { return tool.CallRendering{} },
		).State("remembered", func(value json.RawMessage) error {
			restored = bytes.Clone(value)
			return nil
		}).Run(func(context.Context, struct{}) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "remembered", State: state}, nil
		})
		self.agent = agent.New("", &plainTurnProvider{}, []tool.Tool{subject})
		call, err := subject.Parse(`{}`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = self.tasks.Start(t.Context(), "task-1", subject.Name(), true, false, call.Exec, nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		self.tasksChanged()
		if !bytes.Equal(restored, state) {
			t.Fatalf("completion did not apply tool state: %s", restored)
		}
		stateChanges := 0
		for _, event := range self.recordedEvents {
			if event.Kind == agent.StateChangeEvent {
				stateChanges++
			}
			if event.Kind == taskrecord.RunEnded && bytes.Contains(event.State, []byte(`"tool_state"`)) {
				t.Error("completion duplicated the state-change fact")
			}
		}
		if stateChanges != 1 {
			t.Fatalf("completion recorded %d state changes, want one", stateChanges)
		}
		restored = nil
		resumed := agent.New("", &plainTurnProvider{}, []tool.Tool{subject})
		if err := resumed.RestoreState(self.recordedEvents); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(restored, state) {
			t.Errorf("recorded events lost tool state: %s", restored)
		}
	})
}

func waitForTaskMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(interactiveDeadline)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task did not create its ready marker %s", path)
}

func TestTaskShutdownPersistsStateAndPendingFactsWithoutWaking(t *testing.T) {
	for _, doesFinish := range []bool{false, true} {
		t.Run(fmt.Sprintf("finished=%t", doesFinish), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var screenOutput bytes.Buffer
				self := testConversation(t, &screenOutput)
				self.tasks = tasks.New(t.Context())
				defer self.tasks.Close()
				self.tasksDoWake = true
				var restored json.RawMessage
				state := json.RawMessage(`{"saved":true}`)
				subject := tool.Implement(tool.Definition{Name: "remember"}, func(struct{}) tool.CallRendering { return tool.CallRendering{} }).State("remembered", func(value json.RawMessage) error {
					restored = bytes.Clone(value)
					return nil
				}).Run(func(ctx context.Context, _ struct{}) (tool.ToolCallResult, error) {
					_, _ = tool.LiveOutput(ctx).Write([]byte("saved partial output"))
					if !doesFinish {
						<-ctx.Done()
						return tool.ToolCallResult{State: state}, ctx.Err()
					}
					return tool.ToolCallResult{Output: "completed output", State: state}, nil
				})
				provider := &plainTurnProvider{}
				self.agent = agent.New("", provider, []tool.Tool{subject})
				call, err := subject.Parse(`{}`)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := self.tasks.Start(t.Context(), "worker", subject.Name(), true, false, call.Exec, nil); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				before := screenOutput.String()
				if err := self.endTasks(); err != nil {
					t.Fatal(err)
				}
				if self.currentTurn.Running() || screenOutput.String() != before || len(provider.items) != 0 {
					t.Error("shutdown started a model turn, drew a notice, or changed provider history")
				}
				if !bytes.Equal(restored, state) {
					t.Errorf("shutdown lost returned tool state: %s", restored)
				}
				listing, isRecorded := taskrecord.LastRecorded(self.recordedEvents)
				if !isRecorded || len(listing) != 1 || listing[0].Output == "" {
					t.Fatalf("shutdown lost the task output: %+v", listing)
				}
				pending := taskrecord.PendingNotices(self.recordedEvents)
				if len(pending) != 1 {
					t.Fatalf("shutdown did not retain its undelivered notice: %+v", pending)
				}
				if doesFinish && pending[0].Kind != taskrecord.RunEnded || !doesFinish && pending[0].Kind != taskrecord.EndedWithSession {
					t.Errorf("shutdown misclassified its pending notice: %+v", pending)
				}
			})
		})
	}
}

func TestADiscardedTaskTakesBackItsPendingNotice(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.tasks = tasks.New(t.Context())
	defer self.tasks.Close()
	built := tasks.Snapshot{ID: "build", ToolName: "bash", Run: 1, State: tasks.Done, Output: "built ok\n"}
	tested := tasks.Snapshot{ID: "test", ToolName: "bash", Run: 1, State: tasks.Done, Output: "tests passed\n"}
	self.restoreTasks([]agent.Event{
		taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: built, IsFinal: true}),
		taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: tested, IsFinal: true}),
		taskrecord.ListingEvent([]tasks.Snapshot{built, tested}),
	})

	if _, err := self.tasks.Discard(t.Context(), "build"); err != nil {
		t.Fatal(err)
	}
	self.tasksChanged()
	self.settleAccess()

	notes := noteTexts(self.takeSettledNotes())
	if strings.Contains(notes, "build") || !strings.Contains(notes, "tests passed") {
		t.Errorf("got %q, want only the kept task announced", notes)
	}
}

func TestATaskDiscardedBeforeAResumeIsNotAnnouncedAfterIt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.tasks = tasks.New(t.Context())
	defer self.tasks.Close()
	built := tasks.Snapshot{ID: "build", ToolName: "bash", Run: 1, State: tasks.Done, Output: "built ok\n"}
	self.restoreTasks([]agent.Event{
		taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: built, IsFinal: true}),
		taskrecord.ListingEvent([]tasks.Snapshot{built}),
		taskrecord.ListingEvent(nil),
	})
	self.settleAccess()

	if notes := noteTexts(self.takeSettledNotes()); strings.Contains(notes, "build") {
		t.Errorf("got %q, want the discarded task left unannounced", notes)
	}
}

func TestATaskEndingAsATurnFinishesStillWakesTheConversation(t *testing.T) {
	for _, doesWake := range []bool{true, false} {
		t.Run(fmt.Sprintf("wakes=%t", doesWake), func(t *testing.T) {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.tasksDoWake = doesWake
			self.settleAccess()
			self.start("build it")
			for report := range self.currentTurn.Events() {
				self.takeTurn(report)
			}

			self.taskEnded(tasks.Conclusion{
				Snapshot: tasks.Snapshot{ID: "build", ToolName: "bash", Run: 1, State: tasks.Done, Output: "built\n"},
				IsFinal:  true,
			})
			self.finish()

			if self.currentTurn.Running() != doesWake {
				t.Errorf("got a running turn %t, want %t", self.currentTurn.Running(), doesWake)
			}
		})
	}
}

func TestAResumedSessionNeverReusesADiscardedTaskID(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.tasks = tasks.New(t.Context())
	defer self.tasks.Close()
	first := tasks.Snapshot{ID: "bash-1", ToolName: "bash", Run: 1, State: tasks.Done}
	discarded := tasks.Snapshot{ID: "bash-3", ToolName: "bash", Run: 1, State: tasks.Done}
	self.restoreTasks([]agent.Event{
		taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: discarded, IsFinal: true}),
		taskrecord.ListingEvent([]tasks.Snapshot{first, discarded}),
		taskrecord.ListingEvent([]tasks.Snapshot{first}),
	})

	snapshot, err := self.tasks.Start(t.Context(), "", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != "bash-4" {
		t.Errorf("got %s, want bash-4", snapshot.ID)
	}
}
