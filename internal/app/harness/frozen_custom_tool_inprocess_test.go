package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
)

func TestInProcessFrozenCustomToolRunsCompatibleRevisionAndRefusesNewVersion(t *testing.T) {
	directory := t.TempDir()
	script := declaredToolScript(t)
	writeScript := func(contents string) {
		t.Helper()
		if err := os.WriteFile(script, []byte("#!/bin/bash\nset -euo pipefail\n"+contents+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig := func(version int, description string) []tool.Tool {
		t.Helper()
		path := filepath.Join(directory, "config.toml")
		body := fmt.Sprintf("version = %d\n[tool.weather]\nversion = %d\ndescription = %q\ncommand = [%q]\npermission = \"allow\"\n", config.Format, version, description, script)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		settings, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := settings.BuildCustomTools(command.Options{Workspace: directory, Scratch: directory, Session: "session"})
		if err != nil {
			t.Fatal(err)
		}
		return tools
	}

	writeScript("echo original")
	original := writeConfig(1, "report the weather")
	sessionsDirectory := t.TempDir()
	log, err := store.Create(sessionsDirectory, store.Meta{ToolDefinitions: store.FreezeTools(original)})
	if err != nil {
		t.Fatal(err)
	}
	initial := preparedTools{registeredTools: original, offeredTools: original}
	_, firstProvider := runFrozenCustomToolTurn(t, log, nil, initial, "The original tool ran.")
	if len(firstProvider.toolResults) != 1 || firstProvider.toolResults[0].Output != "original" {
		t.Fatalf("the original command did not reach the model: %+v", firstProvider.toolResults)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Read(sessionsDirectory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	frozenDefinitions := stored.Meta.ToolDefinitions
	if len(frozenDefinitions) != 1 || frozenDefinitions[0].Description != "report the weather" {
		t.Fatalf("got stored frozen definitions %+v", frozenDefinitions)
	}

	writeScript("echo changed implementation")
	compatible, err := prepareFrozenTools(frozenDefinitions, stored.Events, writeConfig(1, "forecast the weather"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(compatible.compatibleNames) != 1 || compatible.offeredTools[0].Description() != "report the weather" || compatible.availabilityRestoration.IsChanged {
		t.Fatalf("same-version implementation was not restored against its stored definition: %+v", compatible)
	}
	log, err = sessions.OpenWriter(sessionsDirectory, stored, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	compatibleScreen, compatibleProvider := runFrozenCustomToolTurn(t, log, stored, compatible, "The compatible implementation ran.")
	if strings.Contains(compatibleScreen, "weather tool changed") {
		t.Errorf("a compatible implementation showed a version warning: %q", compatibleScreen)
	}
	if len(compatibleProvider.toolResults) != 1 || compatibleProvider.toolResults[0].Output != "changed implementation" {
		t.Errorf("the compatible implementation did not reach the model: %+v", compatibleProvider.toolResults)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err = store.Read(sessionsDirectory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	versionTwo, err := prepareFrozenTools(stored.Meta.ToolDefinitions, stored.Events, writeConfig(2, "forecast the weather"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(versionTwo.compatibleNames) != 0 || !versionTwo.availabilityRestoration.IsChanged {
		t.Fatalf("version two remained compatible or did not produce a change: %+v", versionTwo)
	}
	log, err = sessions.OpenWriter(sessionsDirectory, stored, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	changedScreen, changedProvider := runFrozenCustomToolTurn(t, log, stored, versionTwo, "The new version was disabled.")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changedScreen, "weather tool changed from version 1 to version 2") {
		t.Errorf("the resumed session did not show the version warning: %q", changedScreen)
	}
	if len(changedProvider.toolResults) != 1 || !strings.Contains(changedProvider.toolResults[0].Output, "disabled for the remainder of this conversation") {
		t.Errorf("the model was not told the new version was disabled: %+v", changedProvider.toolResults)
	}
}

func runFrozenCustomToolTurn(t *testing.T, writer *store.Writer, stored *store.Session, prepared preparedTools, answer string) (string, *frozenToolProvider) {
	t.Helper()
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	provider := &frozenToolProvider{answer: answer}
	self.agent = agent.NewWithEnabledTools("", provider, prepared.registeredTools, prepared.offeredTools)
	self.recorder = record.New(writer)
	if stored != nil {
		self.restore(stored)
	}
	if prepared.availabilityRestoration.IsChanged {
		self.pendingNotices.add(prepared.availabilityRestoration.Change)
	}
	self.start("run weather")
	for event := range self.currentTurn.Events() {
		self.takeTurn(event)
	}
	self.finish()
	if !strings.Contains(screenOutput.String(), answer) {
		t.Errorf("the turn did not show its answer %q: %q", answer, screenOutput.String())
	}
	return screenOutput.String(), provider
}

type frozenToolProvider struct {
	turn        int
	answer      string
	messages    []string
	toolResults []agent.ToolCallResult
}

func (*frozenToolProvider) Configure(string, []tool.Definition) {}

func (self *frozenToolProvider) AddUserMessage(message string) {
	self.messages = append(self.messages, message)
}

func (self *frozenToolProvider) AddToolResults(results []agent.ToolCallResult) {
	self.toolResults = append(self.toolResults, results...)
}

func (self *frozenToolProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.turn++
	if self.turn == 1 {
		return agent.Reply{Calls: []agent.ToolCall{{ID: "weather-call", Name: "weather", Arguments: `{}`}}}, nil
	}
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: self.answer}) {
		return agent.Reply{}, nil
	}
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}
	return agent.Reply{}, nil
}

func (*frozenToolProvider) Dump() []json.RawMessage { return nil }
func (*frozenToolProvider) Load([]json.RawMessage)  {}
