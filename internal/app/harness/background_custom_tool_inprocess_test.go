package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/interaction"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
	"crdx.org/oh/pkg/toolbox/task"
)

func TestInProcessBackgroundCustomToolCompletionWakesTheModel(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "finish")
	script := declaredToolScript(t)
	if err := os.WriteFile(script, []byte("#!/bin/bash\nset -euo pipefail\nwhile [ ! -e \"$1\" ]; do sleep 0.02; done\nprintf 'forecast complete\\n'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("version = %d\n[tool.weather]\ndescription = \"report the weather\"\ncommand = [%q, %q]\npermission = \"allow\"\nbackground = true\ngroup = \"a\"\n", config.Format, script, gate)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	tasksManager := tasks.New(t.Context())
	defer tasksManager.Close()
	customTools, err := settings.BuildCustomTools(command.Options{
		Workspace:      t.TempDir(),
		Scratch:        t.TempDir(),
		Session:        "weather-session",
		IsGroupAllowed: func(string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	toolboxTools, err := backgroundTools(append(customTools, task.New(tasksManager)), backgroundOptions{
		settings: settings,
		manager:  tasksManager,
		isYolo:   true,
		policyFor: func(name string) func(context.Context, string) (any, error) {
			return taskPolicyFor(name, "a", "", shell.Roots{}, nil, nil, true)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &backgroundWakeProvider{turns: make(chan int, 3)}
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.agent = agent.New("", provider, toolboxTools)
	self.experimental = experimental.New(settings.Experimental)
	self.tasks = tasksManager
	self.tasksDoWake = true

	controller, terminal := ptytest.OpenSized(t, interactiveColumns, interactiveRows)
	restoreTerminal, err := tty.Raw(terminal, terminal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restoreTerminal)
	keyboard := interaction.NewKeyboard(terminal)
	t.Cleanup(keyboard.Release)
	self.screen = output.NewTerminalOfSize(terminal, interactiveColumns, interactiveRows)
	restoreCursor := self.screen.BeginEditing()
	t.Cleanup(restoreCursor)
	inputLine := edit.NewInput(edit.NewHistory("", historyLimit))
	self.show(inputLine)
	self.start("get the weather in the background")
	session := &interactiveSession{
		Transcript: ptytest.Record(controller),
		t:          t,
		terminal:   terminal,
		typing:     controller,
		exited:     make(chan error, 1),
	}
	go func() {
		interaction.Run(keyboard, self.nextRefresh, interaction.Handler{
			GetTurnEvents: func() <-chan turn.Event { return self.currentTurn.Events() },
			OnTurn:        self.takeTurn,
			OnTurnFinished: func() bool {
				self.finish()
				return provider.turn.Load() < 3
			},
			TaskChanges:    self.taskChanges(),
			OnTasksChanged: self.tasksChanged,
			OnDraw:         func() { self.drawAfterEvent(inputLine) },
		})
		session.exited <- nil
	}()
	awaitProviderTurn(t, provider, 1)
	session.waitFor("Started.")
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	session.waitFor("Completed.")
	session.requireShown("Task weather-1 finished after ")
	session.requireHidden("forecast complete")
	session.waitToExit()
	if !strings.Contains(strings.Join(provider.messages, "\n"), "forecast complete") {
		t.Errorf("the completion output did not reach the model: %q", provider.messages)
	}
	if !strings.Contains(strings.Join(provider.messages, "\n"), "Task `weather-1` finished after ") {
		t.Errorf("the task completion notice did not reach the model: %q", provider.messages)
	}
	if !containsToolDefinition(provider.definitions, "weather") || !containsToolDefinition(provider.definitions, "task") {
		t.Errorf("model was not offered custom tool with task control: %+v", provider.definitions)
	}
	if len(provider.answers) != 2 || provider.answers[0] != "Started." || provider.answers[1] != "Completed." {
		t.Errorf("model replies were %q", provider.answers)
	}
	results := self.tasks.List()
	if len(results) != 1 || results[0].State != tasks.Done || !strings.Contains(results[0].Output, "forecast complete") {
		t.Errorf("background command result was not retained: %+v", results)
	}
	if len(provider.toolResults) != 1 || !strings.Contains(provider.toolResults[0].Output, `backgrounded (named "weather-1"); end your turn to wait`) {
		t.Errorf("background call returned unexpected result: %+v", provider.toolResults)
	}
}

type backgroundWakeProvider struct {
	turn        atomic.Int32
	turns       chan int
	definitions []tool.Definition
	messages    []string
	answers     []string
	toolResults []agent.ToolCallResult
}

func (self *backgroundWakeProvider) Configure(_ string, definitions []tool.Definition) {
	self.definitions = definitions
}

func (self *backgroundWakeProvider) AddUserMessage(message string) {
	self.messages = append(self.messages, message)
}

func (self *backgroundWakeProvider) AddToolResults(results []agent.ToolCallResult) {
	self.toolResults = append(self.toolResults, results...)
}

func (self *backgroundWakeProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	turn := self.turn.Add(1)
	self.turns <- int(turn)
	if turn == 1 {
		return agent.Reply{Calls: []agent.ToolCall{{ID: "weather-call", Name: "weather", Arguments: `{"background":true}`}}}, nil
	}
	answer := "Started."
	if turn == 3 {
		answer = "Completed."
	}
	self.answers = append(self.answers, answer)
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: answer}) || !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}
	return agent.Reply{}, nil
}

func (self *backgroundWakeProvider) Dump() []json.RawMessage { return nil }
func (self *backgroundWakeProvider) Load([]json.RawMessage)  {}

func containsToolDefinition(definitions []tool.Definition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func awaitProviderTurn(t *testing.T, provider *backgroundWakeProvider, expected int) {
	t.Helper()
	select {
	case actual := <-provider.turns:
		if actual != expected {
			t.Fatalf("provider started turn %d, want %d", actual, expected)
		}
	case <-time.After(interactiveDeadline):
		t.Fatalf("provider did not receive turn %d", expected)
	}
}
