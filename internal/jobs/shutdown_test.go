package jobs

import (
	"context"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/sandbox"
)

type stubbornRunner struct{}

func (stubbornRunner) Run(context.Context, string, string, sandbox.Policy) (sandbox.Result, error) {
	return sandbox.Result{}, nil
}

func (stubbornRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return &stubbornCommand{over: make(chan struct{})}, nil
}

type stubbornCommand struct {
	over     chan struct{}
	stopOnce sync.Once
}

func (self *stubbornCommand) Wait() (sandbox.Result, error) {
	<-self.over

	return sandbox.Result{}, nil
}

func (self *stubbornCommand) Signal(syscall.Signal) error { return nil }

func (self *stubbornCommand) Stop() {
	self.stopOnce.Do(func() { close(self.over) })
}

func TestStoppingPatienceDependsOnWhyTheJobEnds(t *testing.T) {
	for name, testCase := range map[string]struct {
		patience time.Duration
		end      func(*Manager) error
	}{
		"explicit stop": {
			patience: normalGracePeriod,
			end: func(manager *Manager) error {
				_, err := manager.Stop("stubborn")
				return err
			},
		},
		"session close": {
			patience: shutdownGracePeriod,
			end:      func(manager *Manager) error { return manager.Close() },
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager := New(stubbornRunner{})
				if _, err := manager.Start(t.Context(), "stubborn", t.TempDir(), "serve", sandbox.Policy{}); err != nil {
					t.Fatal(err)
				}

				startedAt := time.Now()
				if err := testCase.end(manager); err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(startedAt); elapsed != testCase.patience {
					t.Errorf("ending took %s, want %s", elapsed, testCase.patience)
				}
			})
		})
	}
}

func TestAJobAlreadyStoppingIsNotStoppedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := New(stubbornRunner{})
		defer func() { _ = manager.Close() }()

		policy := sandbox.Policy{Write: []string{"/workspace"}}
		if _, err := manager.Start(t.Context(), "web", ".", "webd", policy); err != nil {
			t.Fatal(err)
		}

		holdsWorkspace := func(sandbox.Policy) bool { return true }

		first := manager.StopHolding(holdsWorkspace)
		if len(first) != 1 || first[0] != "web" {
			t.Fatalf("got %v, want the one job that held the workspace", first)
		}

		if again := manager.StopHolding(holdsWorkspace); len(again) != 0 {
			t.Errorf("got %v, want a job already stopping to be reported once", again)
		}
	})
}
