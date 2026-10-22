package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestBackgroundAndForegroundShellCallsKeepTheirResultsAcrossResume(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "finish")
	command := fmt.Sprintf("while [ ! -e %q ]; do sleep 0.02; done; printf background-done", gate)
	rig, endpoint := newEndpointRig(t, sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "bash", Arguments: fmt.Sprintf(`{"command":%q,"intent":"Running the background shell completion check","background":true}`, command)}}},
			{Calls: []sim.Call{{Name: "bash", Arguments: `{"command":"printf foreground-done","intent":"Running the foreground shell completion check"}`}}},
			{Say: "Both paths are ready."},
			{Say: "The background task completed."},
			{Calls: []sim.Call{{Name: "task", Arguments: `{"action":"output","name":"task-1"}`}}},
			{Say: "Stored background output is still available."},
		},
	}))
	session := rig.start("--yolo", "-m", "opencode-go/fake", "-t", "bash", "compare background and foreground shell execution")
	session.waitFor("Both paths are ready.")
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	session.waitFor("The background task completed.")
	session.requireShown("background-done")
	session.quit()

	stored := rig.storedSessions()
	if len(stored) != 1 {
		t.Fatalf("got %d sessions, want one", len(stored))
	}
	for _, definition := range stored[0].Meta.ToolDefinitions {
		if definition.Name != "task" {
			continue
		}
		if len(definition.Parameters) != 2 {
			t.Errorf("task offered %d parameters, want action and name", len(definition.Parameters))
		}
		for _, parameter := range definition.Parameters {
			if parameter.Name == "action" && !slices.Equal(parameter.Values, []string{"status", "output", "stop", "list", "prune", "restart", "discard"}) {
				t.Errorf("task offered unexpected actions: %v", parameter.Values)
			}
			if parameter.Name == "seconds" {
				t.Error("task offered the removed wait duration")
			}
		}
	}
	listing, wasRecorded := taskrecord.LastRecorded(stored[0].Events)
	if !wasRecorded || len(listing) != 1 || listing[0].State != tasks.Done || listing[0].Output != "background-done" {
		t.Fatalf("background result was not persisted: %+v", listing)
	}
	requests := endpoint.Requests()
	if len(requests) != 4 || !slices.Contains(requests[0].Tools, "task") {
		t.Fatalf("new session tools or completion wake were wrong: %+v", requests)
	}
	if !strings.Contains(fmt.Sprint(requests[2].Input), "foreground-done") {
		t.Errorf("foreground execution lost its result: %+v", requests[2].Input)
	}

	resumed := rig.start("-r", stored[0].Name, "read the stored task output")
	resumed.waitFor("Stored background output is still available.")
	resumed.quit()
	requests = endpoint.Requests()
	if len(requests) != 6 || !strings.Contains(fmt.Sprint(requests[5].Input), "background-done") {
		t.Errorf("resumed task output did not reach the model: %+v", requests)
	}
}

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

func TestANamedShellTaskStreamsRestartsAfterResumeAndCanBeDiscarded(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "finish")
	ready := filepath.Join(t.TempDir(), "ready")
	command := fmt.Sprintf("printf 'first phase\\n'; touch %q; while [ ! -e %q ]; do sleep 0.02; done; printf 'complete\\n'", ready, gate)
	rig, endpoint := newEndpointRig(t, sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "bash", Arguments: fmt.Sprintf(`{"command":%q,"intent":"Running a named background shell check","background":true,"task_name":"build"}`, command)}}},
			{Say: "Shell background started."},
			{Calls: []sim.Call{{Name: "task", Arguments: `{"action":"output","name":"build"}`}}},
			{Say: "Partial output read."},
			{Say: "Initial run complete."},
			{Calls: []sim.Call{{Name: "task", Arguments: `{"action":"restart","name":"build"}`}}},
			{Say: "Restart requested."},
			{Calls: []sim.Call{{Name: "task", Arguments: `{"action":"discard","name":"build"}`}}},
			{Say: "Task discarded."},
		},
	}))
	session := rig.start("--yolo", "-m", "opencode-go/fake", "-t", "bash", "start and inspect a named shell task")
	session.waitFor("Shell background started.")
	waitForTaskMarker(t, ready)
	session.typeText("read its running output" + pressEnter)
	session.waitFor("Partial output read.")
	requests := endpoint.Requests()
	if len(requests) != 4 || !strings.Contains(fmt.Sprint(requests[3].Input), "first phase") || !strings.Contains(fmt.Sprint(requests[3].Input), "build: running") {
		t.Fatalf("live output did not reach the model: %+v", requests)
	}
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	session.waitFor("Initial run complete.")
	session.quit()
	stored := rig.storedSessions()
	if len(stored) != 1 {
		t.Fatalf("got %d sessions", len(stored))
	}
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ready); err != nil {
		t.Fatal(err)
	}
	resumed := rig.start("-r", stored[0].Name, "restart build using its remembered command")
	resumed.waitFor("Restart requested.")
	waitForTaskMarker(t, ready)
	requests = endpoint.Requests()
	if len(requests) != 7 || !strings.Contains(fmt.Sprint(requests[6].Input), "run 2") {
		t.Fatalf("restart did not reuse the named invocation: %+v", requests)
	}
	resumed.typeText("discard build" + pressEnter)
	resumed.waitFor("Task discarded.")
	resumed.quit()
	listing, isRecorded := taskrecord.LastRecorded(rig.storedSessions()[0].Events)
	if !isRecorded || len(listing) != 0 {
		t.Errorf("discarded task remained in the listing: %+v", listing)
	}
	if requests := endpoint.Requests(); len(requests) != 9 {
		t.Errorf("discard caused an unwanted completion wake: %d requests", len(requests))
	}
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

func TestAnUnconfinedBackgroundServerReportsItsReachablePort(t *testing.T) {
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenerAddress, isTCPAddress := listener.Addr().(*net.TCPAddr)
	if !isTCPAddress {
		t.Fatalf("listener returned a non-TCP address: %v", listener.Addr())
	}
	port := listenerAddress.Port
	if port < 1 || port > 65535 {
		t.Fatalf("listener returned an invalid port: %d", port)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("python3 -B -m http.server %d --bind 127.0.0.1", port)
	rig, endpoint := newEndpointRig(t, sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "bash", Arguments: fmt.Sprintf(`{"command":%q,"intent":"Serving the background task port forwarding check","background":true,"task_name":"docs","forward_port":%d}`, command, port)}}},
			{Say: "Server started."},
			{Calls: []sim.Call{{Name: "task", Arguments: `{"action":"discard","name":"docs"}`}}},
			{Say: "Server discarded."},
		},
	}))
	session := rig.start("--yolo", "-m", "opencode-go/fake", "-t", "bash", "start a background server and publish its port")
	session.waitFor("Server started.")
	stored := rig.storedSessions()
	if len(stored) != 1 {
		t.Fatalf("got %d sessions", len(stored))
	}
	address := "http://" + net.JoinHostPort(portgrant.LocalHost, strconv.Itoa(port))
	requests := endpoint.Requests()
	if len(requests) != 2 || !strings.Contains(fmt.Sprint(requests[1].Input), address) {
		t.Fatalf("the launch did not return a forwarded URL: %+v", requests)
	}
	client := &http.Client{Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	isReachable := false
	deadline := time.Now().Add(interactiveDeadline)
	for !isReachable && time.Now().Before(deadline) {
		response, err := client.Do(request)
		if err == nil {
			isReachable = response.StatusCode == http.StatusOK
			_ = response.Body.Close()
		}
		if !isReachable {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !isReachable {
		t.Fatalf("the user address %s did not reach the background server", address)
	}
	session.typeText("discard docs" + pressEnter)
	session.waitFor("Server discarded.")
	session.quit()
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
	first := tasks.Snapshot{ID: "task-1", ToolName: "bash", Run: 1, State: tasks.Done}
	discarded := tasks.Snapshot{ID: "task-3", ToolName: "bash", Run: 1, State: tasks.Done}
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
	if snapshot.ID != "task-4" {
		t.Errorf("got %s, want task-4", snapshot.ID)
	}
}
