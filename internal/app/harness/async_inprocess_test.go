package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/interaction"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/async"
	"crdx.org/oh/pkg/toolbox/task"
)

func taskFlowShellTool(t *testing.T, manager *tasks.Manager) tool.Tool {
	t.Helper()
	workspace := work.At(reachableWorkspaceDir(t))
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	mode := caps.NewMode(caps.Unconfined())
	roots, err := shell.OpenRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roots.Close() })
	state, err := buildAgentState(agentOptions{
		workspace:        workspace,
		mode:             mode,
		roots:            roots,
		isUnconfined:     true,
		refuseHomeWrite:  allowWrite,
		sessionDirectory: t.TempDir(),
		ensurePersisted:  func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	shellTool := state.shell(func(context.Context, string, string) error { return nil }, sandbox.Direct())
	isBackgroundable := true
	tools, err := backgroundTools([]tool.Tool{shellTool, task.New(manager)}, backgroundOptions{
		settings: config.Config{Tools: map[string]config.ToolConfig{"bash": {IsBackgroundable: &isBackgroundable}}},
		manager:  manager,
		isYolo:   true,
		policyFor: func(name string) func(context.Context, string) (any, error) {
			return taskPolicyFor(name, "", workspace.GetDir(), roots, state.access, mode, true)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tools[0]
}

type taskFlowProviderTurn struct {
	calls     []agent.ToolCall
	say       string
	waitUntil <-chan struct{}
}

type taskFlowProviderRequest struct {
	inputs   []agent.ToolCallResult
	messages []json.RawMessage
}

type taskFlowProvider struct {
	mutex    sync.Mutex
	turns    []taskFlowProviderTurn
	nextTurn int
	inputs   []agent.ToolCallResult
	messages []json.RawMessage
	requests []taskFlowProviderRequest
	tools    []tool.Definition
}

func (self *taskFlowProvider) Configure(_ string, tools []tool.Definition) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.tools = slices.Clone(tools)
}

func (self *taskFlowProvider) AddUserMessage(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.messages = append(self.messages, fmt.Appendf(nil, `{"role":"user","content":%q}`, text))
}

func (self *taskFlowProvider) AddToolResults(results []agent.ToolCallResult) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.inputs = append(self.inputs, results...)
	for _, result := range results {
		self.messages = append(self.messages, fmt.Appendf(nil, `{"role":"tool","id":%q,"content":%q}`, result.ID, result.Output))
	}
}

func (self *taskFlowProvider) Dump() []json.RawMessage {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.messages)
}

func (self *taskFlowProvider) Load(items []json.RawMessage) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.messages = slices.Clone(items)
}

func (self *taskFlowProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	self.mutex.Lock()
	if self.nextTurn >= len(self.turns) {
		self.mutex.Unlock()
		return agent.Reply{}, errors.New("provider received an unexpected request")
	}
	turn := self.turns[self.nextTurn]
	self.nextTurn++
	self.requests = append(self.requests, taskFlowProviderRequest{
		inputs:   slices.Clone(self.inputs),
		messages: slices.Clone(self.messages),
	})
	self.mutex.Unlock()
	if turn.waitUntil != nil {
		select {
		case <-turn.waitUntil:
		case <-ctx.Done():
			return agent.Reply{}, ctx.Err()
		}
	}
	if turn.say != "" {
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: turn.say}) {
			return agent.Reply{}, nil
		}
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
			return agent.Reply{}, nil
		}
	}
	if len(turn.calls) > 0 {
		recordedCalls := make([]map[string]string, 0, len(turn.calls))
		for _, call := range turn.calls {
			recordedCalls = append(recordedCalls, map[string]string{
				"id":        call.ID,
				"name":      call.Name,
				"arguments": call.Arguments,
			})
		}
		calls, err := json.Marshal(recordedCalls)
		if err != nil {
			return agent.Reply{}, err
		}
		self.mutex.Lock()
		self.messages = append(self.messages, fmt.Appendf(nil, `{"role":"assistant","tool_calls":%s}`, calls))
		self.mutex.Unlock()
	}
	if turn.say != "" {
		self.mutex.Lock()
		self.messages = append(self.messages, fmt.Appendf(nil, `{"role":"assistant","content":%q}`, turn.say))
		self.mutex.Unlock()
	}
	return agent.Reply{Calls: turn.calls}, nil
}

func (self *taskFlowProvider) requestInputs() [][]agent.ToolCallResult {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	requests := make([][]agent.ToolCallResult, len(self.requests))
	for index, request := range self.requests {
		requests[index] = slices.Clone(request.inputs)
	}
	return requests
}

func (self *taskFlowProvider) requestMessages() [][]json.RawMessage {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	requests := make([][]json.RawMessage, len(self.requests))
	for index, request := range self.requests {
		requests[index] = slices.Clone(request.messages)
	}
	return requests
}

func (self *taskFlowProvider) toolDefinitions() []tool.Definition {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.tools)
}

type taskFlowEventLoop struct {
	t            *testing.T
	app          *App
	terminal     *os.File
	typing       *os.File
	restore      func()
	exited       chan struct{}
	finished     chan struct{}
	taskListings chan []tasks.Snapshot
	closed       bool
}

func taskFlowStartEventLoop(t *testing.T, app *App) *taskFlowEventLoop {
	t.Helper()
	controller, terminal := ptytest.OpenSized(t, interactiveColumns, interactiveRows)
	restore, err := tty.Raw(terminal, terminal)
	if err != nil {
		t.Fatal(err)
	}
	keyboard := interaction.NewKeyboard(terminal)
	app.keypresses = keyboard
	history := app.openHistory(filepath.Join(t.TempDir(), "history"))
	app.inputLine = edit.NewInput(history)
	loop := &taskFlowEventLoop{
		t:            t,
		app:          app,
		terminal:     terminal,
		typing:       controller,
		restore:      restore,
		exited:       make(chan struct{}),
		finished:     make(chan struct{}, 8),
		taskListings: make(chan []tasks.Snapshot, 16),
	}
	t.Cleanup(func() {
		if loop.restore != nil {
			loop.restore()
		}
	})
	go func() {
		defer close(loop.exited)
		interaction.Run(keyboard, app.nextRefresh, interaction.Handler{
			GetTurnEvents: func() <-chan turn.Event { return app.currentTurn.Events() },
			OnKey: func(keypress key.Key) bool {
				if keypress.Code == key.Rune && keypress.Value == 4 {
					return false
				}
				return app.handleKeypressAndShowInput(app.inputLine, history, keypress)
			},
			OnTurn: app.takeTurn,
			OnTurnFinished: func() bool {
				app.finish()
				loop.finished <- struct{}{}
				return true
			},
			TaskChanges: app.taskChanges(),
			OnTasksChanged: func() {
				app.tasksChanged()
				loop.taskListings <- app.tasks.List()
			},
			TaskActivity:   app.taskActivity(),
			OnTaskActivity: app.taskActivityChanged,
			OnDraw:         func() {},
		})
	}()
	return loop
}

func (self *taskFlowEventLoop) send(text string) {
	self.t.Helper()
	if _, err := self.typing.WriteString(text + "\r"); err != nil {
		self.t.Fatal(err)
	}
}

func (self *taskFlowEventLoop) close() {
	self.t.Helper()
	if self.closed {
		return
	}
	self.closed = true
	_, _ = self.typing.Write([]byte{4})
	select {
	case <-self.exited:
	case <-time.After(5 * time.Second):
		self.t.Fatal("in-process interaction loop did not exit")
	}
	self.app.keypresses = nil
	self.restore()
	self.restore = nil
	_ = self.terminal.Close()
}

type taskFlowStoredSession struct {
	directory string
	name      string
	path      string
	writer    *store.Writer
	closed    bool
	screen    *bytes.Buffer
}

func (self *taskFlowStoredSession) close() error {
	if self.closed {
		return nil
	}
	self.closed = true
	return self.writer.Close()
}

func (self *taskFlowStoredSession) read(t *testing.T) *store.Session {
	t.Helper()
	if err := self.close(); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Read(self.directory, self.name)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Items) == 0 {
		t.Fatal("provider history was not persisted")
	}
	if strings.Contains(self.screen.String(), "conversation state could not be stored") {
		t.Fatal("App reported a provider-state storage warning")
	}
	return stored
}

func taskFlowNewApp(t *testing.T, manager *tasks.Manager, bash tool.Tool, turns []taskFlowProviderTurn) (*App, *taskFlowProvider, *taskFlowStoredSession) {
	t.Helper()
	var screenOutput bytes.Buffer
	app := testConversation(t, &screenOutput)
	directory := t.TempDir()
	writer, err := store.CreateNamed(directory, "binary-session", store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	stored := &taskFlowStoredSession{
		directory: directory,
		name:      writer.Name(),
		path:      session.Dir(directory, writer.Name()),
		writer:    writer,
		screen:    &screenOutput,
	}
	t.Cleanup(func() { _ = stored.close() })
	app.recorder = record.New(writer)
	app.tasks = manager
	app.tasksDoWake = true
	app.sessionDirectory = stored.path
	provider := &taskFlowProvider{turns: turns}
	app.agent = agent.New("", provider, []tool.Tool{bash, task.New(manager)})
	return app, provider, stored
}

func taskFlowWaitForProviderRequests(t *testing.T, provider *taskFlowProvider, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(provider.requestInputs()) >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider saw fewer than %d requests: %d", count, len(provider.requestInputs()))
}

func taskFlowWaitForTurnToSettle(t *testing.T, loop *taskFlowEventLoop) {
	t.Helper()
	select {
	case <-loop.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not settle")
	}
}

func taskFlowWaitForTaskOutput(t *testing.T, manager *tasks.Manager, id string, run int, contains string) tasks.Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Output(id)
		if err == nil && snapshot.Run == run && strings.Contains(snapshot.Output, contains) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, err := manager.Output(id)
	t.Fatalf("task %s run %d did not produce %q: %+v, %v", id, run, contains, snapshot, err)
	return tasks.Snapshot{}
}

func taskFlowWaitForAssistantInput(t *testing.T, provider *taskFlowProvider, request int, text string) {
	t.Helper()
	taskFlowWaitForProviderRequests(t, provider, request+1)
	for _, result := range provider.requestInputs()[request] {
		if strings.Contains(result.Output, text) {
			return
		}
	}
	t.Fatalf("provider request %d did not contain %q: %+v", request, text, provider.requestInputs()[request])
}

var taskFlowCallSequence atomic.Uint64

func taskFlowCall(name string, arguments string) agent.ToolCall {
	return agent.ToolCall{ID: fmt.Sprintf("call-%s-%d", name, taskFlowCallSequence.Add(1)), Name: name, Arguments: arguments}
}

func TestInProcessBackgroundAndForegroundShellCallsKeepTheirResultsAcrossResume(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "finish")
	command := fmt.Sprintf("while [ ! -e %q ]; do sleep 0.02; done; printf background-done", gate)
	manager := tasks.New(t.Context())
	defer manager.Close()
	bash := taskFlowShellTool(t, manager)
	providerTurns := []taskFlowProviderTurn{
		{calls: []agent.ToolCall{taskFlowCall("bash", fmt.Sprintf(`{"command":%q,"intent":"Running the background shell completion check","background":true}`, command))}},
		{calls: []agent.ToolCall{taskFlowCall("bash", `{"command":"printf foreground-done","intent":"Running the foreground shell completion check"}`)}},
		{say: "Both paths are ready."},
		{calls: []agent.ToolCall{taskFlowCall("task", `{"action":"output","name":"bash-1"}`)}},
		{say: "The background task completed."},
	}
	app, provider, firstSession := taskFlowNewApp(t, manager, bash, providerTurns)
	app.start("compare background and foreground shell execution")
	loop := taskFlowStartEventLoop(t, app)
	defer loop.close()
	taskFlowWaitForProviderRequests(t, provider, 3)
	taskFlowWaitForTurnToSettle(t, loop)
	definitions := provider.toolDefinitions()
	if !slices.ContainsFunc(definitions, func(definition tool.Definition) bool { return definition.Name == "task" }) {
		t.Fatalf("task tool was not offered: %+v", definitions)
	}
	var taskDefinition tool.Definition
	for _, definition := range definitions {
		if definition.Name == "task" {
			taskDefinition = definition
		}
	}
	if len(taskDefinition.Schema) != 2 {
		t.Errorf("task offered %d parameters, want action and name", len(taskDefinition.Schema))
	}
	if !slices.Equal(taskDefinition.Schema.Find("action").Values, []string{"status", "output", "stop", "list", "prune", "restart", "discard"}) {
		t.Errorf("task offered unexpected actions: %v", taskDefinition.Schema.Find("action").Values)
	}
	if taskDefinition.Schema.Find("seconds") != nil {
		t.Error("task offered the removed wait duration")
	}
	taskFlowWaitForAssistantInput(t, provider, 2, "foreground-done")
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	taskFlowWaitForAssistantInput(t, provider, 4, "background-done")
	taskFlowWaitForTurnToSettle(t, loop)
	completionInput := fmt.Sprint(provider.requestMessages()[3])
	if !strings.Contains(completionInput, "bash-1") || !strings.Contains(completionInput, "finished") {
		t.Errorf("App completion notice was not added to model input: %s", completionInput)
	}
	loop.close()
	stored := firstSession.read(t)
	listing, wasRecorded := taskrecord.LastRecorded(stored.Events)
	if !wasRecorded || len(listing) != 1 || listing[0].State != tasks.Done || listing[0].Output != "background-done" {
		t.Fatalf("background result was not persisted: %+v", listing)
	}
	resumedManager := tasks.New(t.Context())
	defer resumedManager.Close()
	resumedBash := taskFlowShellTool(t, resumedManager)
	resumedApp, resumedProvider, _ := taskFlowNewApp(t, resumedManager, resumedBash, []taskFlowProviderTurn{
		{calls: []agent.ToolCall{taskFlowCall("task", `{"action":"output","name":"bash-1"}`)}},
		{say: "Stored background output is still available."},
	})
	if err := resumedApp.agent.Load(stored.Items); err != nil {
		t.Fatal(err)
	}
	resumedApp.restoreTasks(stored.Events)
	resumedApp.start("read the stored task output")
	resumedLoop := taskFlowStartEventLoop(t, resumedApp)
	defer resumedLoop.close()
	taskFlowWaitForAssistantInput(t, resumedProvider, 1, "background-done")
	taskFlowWaitForTurnToSettle(t, resumedLoop)
}

func TestInProcessANamedShellTaskStreamsRestartsAfterResumeAndCanBeDiscarded(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "finish")
	ready := filepath.Join(t.TempDir(), "ready")
	command := fmt.Sprintf("printf 'first phase\\n'; touch %q; while [ ! -e %q ]; do sleep 0.02; done; printf 'complete\\n'", ready, gate)
	manager := tasks.New(t.Context())
	defer manager.Close()
	bash := taskFlowShellTool(t, manager)
	liveOutputReady := make(chan struct{})
	providerTurns := []taskFlowProviderTurn{
		{calls: []agent.ToolCall{taskFlowCall("bash", fmt.Sprintf(`{"command":%q,"intent":"Running a named background shell check","background":true,"task_name":"build"}`, command))}},
		{waitUntil: liveOutputReady, calls: []agent.ToolCall{taskFlowCall("task", `{"action":"output","name":"build"}`)}},
		{say: "Partial output read."},
		{say: "Initial run complete."},
	}
	app, provider, firstSession := taskFlowNewApp(t, manager, bash, providerTurns)
	app.start("start and inspect a named shell task")
	loop := taskFlowStartEventLoop(t, app)
	defer loop.close()
	firstOutput := taskFlowWaitForTaskOutput(t, manager, "build", 1, "first phase")
	deadline := time.Now().Add(5 * time.Second)
	var current tasks.Snapshot
	var currentErr error
	for time.Now().Before(deadline) {
		current, currentErr = taskrecord.Read(firstSession.directory, toolresult.TaskReference{SessionName: firstSession.name, TaskName: "build", Run: 1})
		if currentErr == nil && current.State == tasks.Running && current.Output == firstOutput.Output && current.Invocation != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if currentErr != nil || current.State != tasks.Running || current.Output != firstOutput.Output || current.Invocation == nil {
		t.Fatalf("TaskActivity did not persist current output for the active session: %+v, %v", current, currentErr)
	}
	close(liveOutputReady)
	taskFlowWaitForAssistantInput(t, provider, 2, "first phase")
	taskFlowWaitForAssistantInput(t, provider, 2, "build: running")
	taskFlowWaitForTurnToSettle(t, loop)
	waitForTaskMarker(t, ready)
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	taskFlowWaitForTaskOutput(t, manager, "build", 1, "complete")
	taskFlowWaitForProviderRequests(t, provider, 4)
	taskFlowWaitForTurnToSettle(t, loop)
	completionInput := fmt.Sprint(provider.requestMessages()[3])
	if !strings.Contains(completionInput, "build") || !strings.Contains(completionInput, "finished") {
		t.Errorf("App completion notice was not added to model input: %s", completionInput)
	}
	loop.close()
	stored := firstSession.read(t)
	listing, recorded := taskrecord.LastRecorded(stored.Events)
	if !recorded || len(listing) != 1 || listing[0].State != tasks.Done || listing[0].Invocation == nil || listing[0].Invocation.Arguments == "" {
		t.Fatalf("completed task invocation was not persisted: %+v", listing)
	}
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ready); err != nil {
		t.Fatal(err)
	}
	resumedManager := tasks.New(t.Context())
	defer resumedManager.Close()
	resumedBash := taskFlowShellTool(t, resumedManager)
	async.Bind(resumedBash, resumedBash.Schema())
	resumedApp, resumedProvider, _ := taskFlowNewApp(t, resumedManager, resumedBash, []taskFlowProviderTurn{
		{calls: []agent.ToolCall{taskFlowCall("task", `{"action":"restart","name":"build"}`)}},
		{say: "Restart requested."},
		{calls: []agent.ToolCall{taskFlowCall("task", `{"action":"discard","name":"build"}`)}},
		{say: "Task discarded."},
		{say: "Still responsive."},
	})
	if err := resumedApp.agent.Load(stored.Items); err != nil {
		t.Fatal(err)
	}
	resumedApp.restoreTasks(stored.Events)
	resumedApp.start("restart build using its remembered command")
	resumedLoop := taskFlowStartEventLoop(t, resumedApp)
	defer resumedLoop.close()
	taskFlowWaitForAssistantInput(t, resumedProvider, 1, "run 2")
	taskFlowWaitForTurnToSettle(t, resumedLoop)
	taskFlowWaitForTaskOutput(t, resumedManager, "build", 2, "first phase")
	waitForTaskMarker(t, ready)
	resumedLoop.send("discard build")
	taskFlowWaitForAssistantInput(t, resumedProvider, 3, "discarded")
	taskFlowWaitForTurnToSettle(t, resumedLoop)
	for {
		select {
		case snapshots := <-resumedLoop.taskListings:
			if len(snapshots) == 0 {
				goto discardedListingRecorded
			}
		default:
			time.Sleep(time.Millisecond)
		}
	}
discardedListingRecorded:
	requestsBeforePing := len(resumedProvider.requestInputs())
	if requestsBeforePing != 4 {
		t.Fatalf("unexpected model requests before the explicit post-discard turn: %d", requestsBeforePing)
	}
	resumedLoop.send("check that the session is still responsive")
	taskFlowWaitForProviderRequests(t, resumedProvider, requestsBeforePing+1)
	taskFlowWaitForTurnToSettle(t, resumedLoop)
	if got := len(resumedProvider.requestInputs()); got != requestsBeforePing+1 {
		t.Errorf("discard caused an unwanted completion wake: provider requests %d -> %d", requestsBeforePing, got)
	}
	if !strings.Contains(fmt.Sprint(resumedApp.recordedEvents), "Still responsive.") {
		t.Error("the explicit post-discard user turn did not reach the model")
	}
}

func TestInProcessAnUnconfinedBackgroundServerReportsItsReachablePort(t *testing.T) {
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addressInfo, isTCP := listener.Addr().(*net.TCPAddr)
	if !isTCP {
		t.Fatalf("listener returned a non-TCP address: %v", listener.Addr())
	}
	port := addressInfo.Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	manager := tasks.New(t.Context())
	defer manager.Close()
	bash := taskFlowShellTool(t, manager)
	command := fmt.Sprintf("python3 -B -m http.server %d --bind 127.0.0.1", port)
	call, err := bash.Parse(fmt.Sprintf(`{"command":%q,"intent":"Serving the background task port forwarding check","background":true,"task_name":"docs","forward_port":%d}`, command, port))
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !strings.Contains(result.Output, address) {
		t.Fatalf("launch receipt did not return %s: %q", address, result.Output)
	}
	taskFlowGetServer(t, address)
	if _, err := manager.Discard(t.Context(), "docs"); err != nil {
		t.Fatal(err)
	}
}

func taskFlowGetServer(t *testing.T, address string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the user address %s did not reach the background server", address)
}
