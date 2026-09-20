package tty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

const signalProcessVariable = "OH_TEST_SIGNAL_PROCESS"

const restoredMarker = "the terminal was handed back"

func TestASignalHandsTheTerminalBackAndThenKills(t *testing.T) {
	if os.Getenv(signalProcessVariable) != "" {
		stopListening := RestoreOnSignal(func() {
			fmt.Println(restoredMarker)
			_ = os.Stdout.Sync()
		})
		defer stopListening()

		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}

		time.Sleep(10 * time.Second)

		return
	}

	//nolint:gosec // rerun this test binary as its signalled subprocess
	command := exec.CommandContext(
		t.Context(), os.Args[0], "-test.run=^TestASignalHandsTheTerminalBackAndThenKills$",
	)
	command.Env = append(os.Environ(), signalProcessVariable+"=1")

	output, err := command.CombinedOutput()

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("the signalled process returned %v, having drawn %q", err, string(output))
	}

	status, ok := exitError.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("the signalled process reported %T", exitError.Sys())
	}

	if !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Errorf("the signalled process ended by %v, signalled=%t", status.Signal(), status.Signaled())
	}

	if !strings.Contains(string(output), restoredMarker) {
		t.Errorf("the terminal was not handed back, the process drew %q", string(output))
	}
}

func TestASignalIsAnsweredOnceAndThenPassedOn(t *testing.T) {
	signals := make(chan os.Signal, 1)
	order := make(chan string, 2)

	go watch(
		signals,
		make(chan struct{}),
		func() { order <- "restored" },
		func(received os.Signal) { order <- "raised " + received.String() },
	)

	signals <- syscall.SIGHUP

	if got := <-order; got != "restored" {
		t.Errorf("first came %q", got)
	}

	if got := <-order; got != "raised "+syscall.SIGHUP.String() {
		t.Errorf("second came %q", got)
	}
}

func TestNothingIsHandedBackOnceListeningHasStopped(t *testing.T) {
	stopped := make(chan struct{})
	watched := make(chan struct{})

	go func() {
		defer close(watched)

		watch(
			make(chan os.Signal),
			stopped,
			func() { t.Error("the terminal was handed back without a signal") },
			func(os.Signal) { t.Error("a signal was raised without one arriving") },
		)
	}()

	close(stopped)

	select {
	case <-watched:
	case <-time.After(time.Second):
		t.Error("the listener outlived what stopped it")
	}
}

func TestListeningStopsWithoutTheProcessBeingSignalled(t *testing.T) {
	restored := make(chan struct{})

	stopListening := RestoreOnSignal(func() { close(restored) })
	stopListening()

	select {
	case <-restored:
		t.Error("the terminal was handed back without a signal")
	default:
	}
}
