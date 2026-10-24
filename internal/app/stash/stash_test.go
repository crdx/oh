package stash_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/stash"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func openStore(t *testing.T, directory string) *stash.Store {
	t.Helper()

	store, err := stash.Open(directory, func() error {
		return os.MkdirAll(directory, 0o700)
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStashRestoresEntriesNewestFirstAcrossAReopen(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "session")
	self := openStore(t, directory)
	for _, text := range []string{"first", "second\nover two lines", "  whitespace  "} {
		if _, err := self.Push(text); err != nil {
			t.Fatal(err)
		}
	}

	self = openStore(t, directory)
	var restoredDrafts []string
	for self.Count() > 0 {
		text, _, found, err := self.Exchange("")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("expected an entry")
		}
		restoredDrafts = append(restoredDrafts, text)
	}

	want := []string{"  whitespace  ", "second\nover two lines", "first"}
	if !slices.Equal(restoredDrafts, want) {
		t.Errorf("restored %q, want %q", restoredDrafts, want)
	}
}

func TestExchangeSuspendsTheCurrentDraft(t *testing.T) {
	self := stash.NewMemory()
	if _, err := self.Push("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := self.Push("second"); err != nil {
		t.Fatal(err)
	}

	restoredDraft, count, found, err := self.Exchange("current")
	if err != nil {
		t.Fatal(err)
	}
	if !found || restoredDraft != "second" || count != 2 {
		t.Errorf("exchange returned %q, %d, %t", restoredDraft, count, found)
	}

	restoredDraft, _, found, err = self.Exchange("")
	if err != nil {
		t.Fatal(err)
	}
	if !found || restoredDraft != "current" {
		t.Errorf("restored %q, found=%t, want the suspended draft", restoredDraft, found)
	}
}

func TestAnEmptyStashChangesNothing(t *testing.T) {
	self := stash.NewMemory()

	if text, count, found, err := self.Exchange("current"); err != nil || found || text != "" || count != 0 {
		t.Errorf("empty exchange returned %q, %d, %t, %v", text, count, found, err)
	}
}

func TestAFailedWriteLeavesTheInMemoryStashUnchanged(t *testing.T) {
	directory := t.TempDir()
	failure := errors.New("cannot persist")
	self, err := stash.Open(directory, func() error { return failure })
	if err != nil {
		t.Fatal(err)
	}

	if _, err := self.Push("draft"); !errors.Is(err, failure) {
		t.Fatalf("push returned %v, want %v", err, failure)
	}
	if self.Count() != 0 {
		t.Errorf("failed push left %d entries", self.Count())
	}
}

func TestAStashFromANewerBuildIsRefused(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, stash.FileName), []byte(`{"version":99,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := stash.Open(directory, nil)
	if err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Fatalf("got %v, want a newer-format error", err)
	}
}

func TestAFailedExchangeLeavesTheStoredAndInMemoryStashUnchanged(t *testing.T) {
	directory := t.TempDir()
	failure := errors.New("cannot persist")
	isFailing := false
	self, err := stash.Open(directory, func() error {
		if isFailing {
			return failure
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := self.Push("stashed"); err != nil {
		t.Fatal(err)
	}

	isFailing = true
	if _, _, _, err := self.Exchange("current"); !errors.Is(err, failure) {
		t.Fatalf("exchange returned %v, want %v", err, failure)
	}
	if self.Count() != 1 {
		t.Errorf("failed exchange left %d entries, want 1", self.Count())
	}

	isFailing = false
	reopened := openStore(t, directory)
	restoredDraft, _, found, err := reopened.Exchange("")
	if err != nil {
		t.Fatal(err)
	}
	if !found || restoredDraft != "stashed" {
		t.Errorf("restored %q, found=%t", restoredDraft, found)
	}
}

func TestStashingBeforeTheFirstMessagePreservesTheStartupEvent(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.StartupEvent}); err != nil {
		t.Fatal(err)
	}

	self, err := stash.Open(session.Dir(directory, log.Name()), log.EnsurePersisted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := self.Push("say nothing"); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Events) != 1 || storedSession.Events[0].Kind != agent.StartupEvent {
		t.Errorf("stored events are %+v, want the startup event", storedSession.Events)
	}
}
