package analyse

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

func TestInProcessAnalysisWritesItsCacheOnlyUnderTheOverriddenStateDirectory(t *testing.T) {
	stateDirectory := t.TempDir()
	cacheDirectory := t.TempDir()
	xdgStateDirectory := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDirectory)
	t.Setenv("XDG_CACHE_HOME", cacheDirectory)
	t.Setenv("XDG_STATE_HOME", xdgStateDirectory)

	sessionsDirectory := location.GetSessionsDir()
	writer, err := store.Create(sessionsDirectory, store.Meta{WorkspaceDir: t.TempDir(), Provider: "codex", Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "A question."}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.ModelMessageEvent, Text: "Stored answer.", Usage: &agent.Usage{InputTokens: 10, OutputTokens: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	cachePath := location.GetAnalysisCachePath()
	var output bytes.Buffer
	if err := run(Settings{
		SessionsDir:    sessionsDirectory,
		CachePath:      cachePath,
		ModelCachePath: location.GetModelCachePath(),
		Currency:       money.Dollar(),
	}, console.Output{Screen: &output, Failure: &output}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cachePath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected a populated analysis cache at %s, got %v, %v", cachePath, info, err)
	}
	for _, directory := range []string{cacheDirectory, xdgStateDirectory} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("analysis wrote outside the overridden state directory: %s contains %v", filepath.Clean(directory), entries)
		}
	}
}
