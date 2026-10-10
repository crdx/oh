package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

const testFormat = 1

type testState struct {
	Version int `json:"version"`
	Count   int `json:"count"`
}

func TestAStateFileFromANewerOhIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"count":"not a number now"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Update(path, testFormat, func(state *testState) error {
		t.Error("expected a newer state file not to be handed on")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Fatalf("expected the newer format to be named, got %v", err)
	}
}

func TestUpdateCarriesStateForward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	for range 3 {
		if err := Update(path, testFormat, func(state *testState) error {
			state.Count++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := Update(path, testFormat, func(state *testState) error {
		if state.Count != 3 {
			t.Errorf("got count %d, want 3", state.Count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUpdatesShareTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const updates = 20

	var wait sync.WaitGroup
	for range updates {
		wait.Go(func() {
			if err := Update(path, testFormat, func(state *testState) error {
				state.Count++
				return nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wait.Wait()

	if err := Update(path, testFormat, func(state *testState) error {
		if state.Count != updates {
			t.Errorf("got count %d, want %d", state.Count, updates)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailedUpdateDoesNotReplaceState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := Update(path, testFormat, func(state *testState) error {
		state.Count = 4
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("stop")
	if err := Update(path, testFormat, func(state *testState) error {
		state.Count = 99
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("got %v, want %v", err, failure)
	}

	if err := Update(path, testFormat, func(state *testState) error {
		if state.Count != 4 {
			t.Errorf("got count %d, want 4", state.Count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTryUpdateStandsAsideForWhoeverHoldsTheState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	if err := Update(path, testFormat, func(state *testState) error {
		*state = testState{Version: testFormat, Count: 7}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	held, err := lock(path, syscall.LOCK_EX)
	if err != nil {
		t.Fatal(err)
	}
	defer release(held)

	claimed, err := TryUpdate(path, testFormat, func(*testState) error {
		t.Error("expected held state not to be handed on")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if claimed {
		t.Error("expected the held state to be left alone")
	}

	var state testState
	if err := Read(path, testFormat, &state); err != nil {
		t.Fatal(err)
	}

	if state.Count != 7 {
		t.Errorf("expected what was written to stand, got %+v", state)
	}
}

func TestTryUpdateTakesFreeState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	claimed, err := TryUpdate(path, testFormat, func(state *testState) error {
		*state = testState{Version: testFormat, Count: 3}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if !claimed {
		t.Fatal("expected free state to be taken")
	}

	var state testState
	if err := Read(path, testFormat, &state); err != nil {
		t.Fatal(err)
	}

	if state.Count != 3 {
		t.Errorf("got %+v", state)
	}
}

func TestLockedWorkOnAPlainFileLosesNoConcurrentWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain")
	const writers = 20

	var wait sync.WaitGroup
	for range writers {
		wait.Go(func() {
			if err := Locked(path, func() error {
				data, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				return WriteFile(path, append(data, 'x'))
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wait.Wait()

	data, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != writers {
		t.Errorf("got %d writes, want %d", len(data), writers)
	}
}
