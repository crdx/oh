package hostcommand

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHangingUpStopsACommandThatRefusesTheKill(t *testing.T) {
	refuseKills(t)

	startedAt := time.Now()
	result, processID := stopOnceStarted(t, "echo $$ > started.tmp; mv started.tmp started; exec sleep 60")
	if took := time.Since(startedAt); took >= waitDelay {
		t.Errorf("took %s to stop, want the hang-up to stop it at once", took)
	}
	if !result.IsStoppedByUser {
		t.Errorf("got result %+v, want it stopped by the user", result)
	}
	if !isGone(processID) {
		t.Errorf("the command %d outlived the hang-up", processID)
	}
}

func TestAStopDoesNotWaitOnACommandThatSurvivesItsKill(t *testing.T) {
	refuseKills(t)
	previousDelay := abandonDelay
	abandonDelay = waitDelay / 10
	t.Cleanup(func() { abandonDelay = previousDelay })

	startedAt := time.Now()
	result, processID := stopOnceStarted(t, "trap '' HUP; echo $$ > started.tmp; mv started.tmp started; sleep 60")
	t.Cleanup(func() { _ = syscall.Kill(-processID, syscall.SIGKILL) })

	if took := time.Since(startedAt); took >= waitDelay {
		t.Errorf("took %s to give up, want it to give up after the abandon delay", took)
	}
	if !result.IsStoppedByUser {
		t.Errorf("got result %+v, want it stopped by the user", result)
	}
}

func refuseKills(t *testing.T) {
	t.Helper()

	previous := signalGroup
	signalGroup = func(int, syscall.Signal) error { return syscall.EPERM }
	t.Cleanup(func() { signalGroup = previous })
}

func stopOnceStarted(t *testing.T, command string) (Result, int) {
	t.Helper()

	directory := t.TempDir()
	stopContext, stop := context.WithCancel(t.Context())
	defer stop()

	go func() {
		for stopContext.Err() == nil {
			if _, err := os.Stat(filepath.Join(directory, "started")); err == nil {
				stop()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	result, err := Run(stopContext, directory, command, DefaultTimeLimit, &Output{})
	if err != nil {
		t.Fatal(err)
	}

	started, err := os.ReadFile(filepath.Join(directory, "started")) //nolint:gosec // the test made this directory
	if err != nil {
		t.Fatal(err)
	}
	processID, err := strconv.Atoi(strings.TrimSpace(string(started)))
	if err != nil {
		t.Fatal(err)
	}

	return result, processID
}

func isGone(processID int) bool {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(processID), "stat"))
		if err != nil {
			return true
		}
		if fields := strings.Fields(string(status)); len(fields) > 2 && fields[2] == "Z" {
			return true
		}
	}

	return false
}
