package tty

import (
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func TestAStoppedReaderLeavesTheTerminalToTheNextReader(t *testing.T) {
	terminal := pty(t)

	reader := NewReader(terminal)
	t.Cleanup(reader.Close)

	stopped := make(chan error, 1)

	go func() {
		var buffer [1]byte
		_, err := reader.Read(buffer[:])
		stopped <- err
	}()

	time.Sleep(2 * readPollInterval)

	stoppedAt := time.Now()
	reader.Stop()

	select {
	case err := <-stopped:
		if !errors.Is(err, io.EOF) {
			t.Errorf("got %v, want EOF", err)
		}
		if waited := time.Since(stoppedAt); waited > readPollInterval/2 {
			t.Errorf("the read waited %v to stop, so it sat out a poll rather than being woken", waited)
		}
	case <-time.After(time.Second):
		t.Fatal("a stopped read did not return")
	}

	if _, err := io.WriteString(terminal, "x"); err != nil {
		t.Fatal(err)
	}

	var taken [1]byte
	if _, err := terminal.Read(taken[:]); err != nil {
		t.Fatal(err)
	}
	if taken[0] != 'x' {
		t.Errorf("the next reader got %q, so the stopped one was still holding the terminal", taken[0])
	}
}

func TestAReaderWithoutAWakePipeStillStops(t *testing.T) {
	terminal := pty(t)

	reader := &Reader{input: terminal, stopSignal: make(chan struct{})}
	stopped := make(chan error, 1)

	go func() {
		var buffer [1]byte
		_, err := reader.Read(buffer[:])
		stopped <- err
	}()

	time.Sleep(readPollInterval / 2)
	reader.Stop()

	select {
	case err := <-stopped:
		if !errors.Is(err, io.EOF) {
			t.Errorf("got %v, want EOF", err)
		}
	case <-time.After(2 * readPollInterval):
		t.Fatal("a read without a wake pipe did not stop within a poll")
	}
}

func TestAStoppedReaderTakesNothingFurtherFromTheTerminal(t *testing.T) {
	terminal := pty(t)

	reader := NewReader(terminal)
	t.Cleanup(reader.Close)
	reader.Stop()

	if _, err := io.WriteString(terminal, "x"); err != nil {
		t.Fatal(err)
	}

	var buffer [1]byte
	if _, err := reader.Read(buffer[:]); !errors.Is(err, io.EOF) {
		t.Errorf("got %v, want EOF", err)
	}

	var taken [1]byte
	if _, err := terminal.Read(taken[:]); err != nil {
		t.Fatal(err)
	}
	if taken[0] != 'x' {
		t.Errorf("the next reader got %q, so the stopped one had taken the byte", taken[0])
	}
}

func TestStoppingIsAnnouncedOnceHoweverOftenItIsAskedFor(t *testing.T) {
	reader := NewReader(pty(t))
	t.Cleanup(reader.Close)

	select {
	case <-reader.Stopping():
		t.Fatal("a reader announced stopping before it was stopped")
	default:
	}

	var stoppers sync.WaitGroup
	for range 4 {
		stoppers.Go(reader.Stop)
	}

	stoppers.Wait()

	select {
	case <-reader.Stopping():
	case <-time.After(time.Second):
		t.Fatal("stopping was never announced")
	}
}

func TestAHungUpTerminalEndsTheReadRatherThanSpinning(t *testing.T) {
	input, hangUp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })

	reader := NewReader(input)
	t.Cleanup(reader.Close)

	_ = hangUp.Close()

	ended := make(chan error, 1)
	go func() {
		var buffer [1]byte
		_, readError := reader.Read(buffer[:])
		ended <- readError
	}()

	select {
	case readError := <-ended:
		if !errors.Is(readError, io.EOF) {
			t.Errorf("got %v, want EOF", readError)
		}
	case <-time.After(time.Second):
		t.Fatal("a hung-up terminal never ended the read, so the poll is spinning on it")
	}
}

func TestAReaderHandsOnWhatTheTerminalSends(t *testing.T) {
	terminal := pty(t)

	if _, err := io.WriteString(terminal, "hi"); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(terminal)
	t.Cleanup(reader.Close)

	var buffer [2]byte
	read, err := io.ReadFull(reader, buffer[:])
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buffer[:read]); got != "hi" {
		t.Errorf("got %q", got)
	}
}

func TestAReaderWaitsForWhatTheTerminalHasYetToSend(t *testing.T) {
	terminal := pty(t)

	reader := NewReader(terminal)
	t.Cleanup(reader.Close)

	go func() {
		time.Sleep(2 * readPollInterval)
		_, _ = io.WriteString(terminal, "late")
	}()

	var buffer [4]byte
	if _, err := io.ReadFull(reader, buffer[:]); err != nil {
		t.Fatal(err)
	}
	if got := string(buffer[:]); got != "late" {
		t.Errorf("got %q", got)
	}
}

func TestClosingAReaderReleasesItsWakePipeAndCanBeAskedForTwice(t *testing.T) {
	reader := NewReader(pty(t))
	if reader.wakeRead == nil || reader.wakeWrite == nil {
		t.Fatal("expected a wake pipe")
	}

	descriptor := reader.wakeRead.Fd()

	reader.Stop()
	reader.Close()
	reader.Close()

	var status [1]byte
	if _, err := os.NewFile(descriptor, "wake").Read(status[:]); err == nil {
		t.Error("the wake pipe was still open after closing")
	}
}
