package ctl

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestSessionListingsDoNotNeedTheDataLayoutLock(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())
	if err := os.Mkdir(location.GetDataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(location.GetDataDir("layout.lock"), 0o700); err != nil {
		t.Fatal(err)
	}

	directory := location.GetSessionsDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}

	for _, arguments := range [][]string{{"sessions", "--json"}, {"sessions", "--running"}} {
		if code := Run(arguments, nil); code != 0 {
			t.Errorf("%v exited with %d", arguments, code)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"sessions", "--delete", writer.Name()}, nil); code != 1 {
		t.Errorf("session deletion exited with %d, want 1", code)
	}
	if !session.Exists(directory, writer.Name()) {
		t.Error("the session was deleted without checking the data layout")
	}
}

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", usage)
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
