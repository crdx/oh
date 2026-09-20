package tty

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestReadLineReadsOnlyOneLine(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	if _, err := writer.WriteString("redirect\r\nnext\n"); err != nil {
		t.Fatal(err)
	}
	line, err := ReadLine(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if line != "redirect" {
		t.Errorf("got %q", line)
	}
}

func TestReadLineStopsWhenCancelled(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := ReadLine(ctx, reader)
		result <- err
	}()
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled line read remained blocked")
	}
}

func TestReadLineTakesNothingWhenItIsCancelledAlready(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	if _, err := writer.WriteString("waiting\n"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ReadLine(ctx, reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}

	line, err := ReadLine(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if line != "waiting" {
		t.Errorf("got %q, so the cancelled read had taken from the line", line)
	}
}

func TestReadLineHandsBackWhatArrivedBeforeTheEnd(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	if _, err := writer.WriteString("partial"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()

	line, err := ReadLine(t.Context(), reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want EOF", err)
	}
	if line != "partial" {
		t.Errorf("got %q", line)
	}
}

func TestReadLineStopsWhenCancelledMidWait(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)

	go func() {
		_, readError := ReadLine(ctx, reader)
		result <- readError
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case readError := <-result:
		if !errors.Is(readError, context.Canceled) {
			t.Errorf("got %v", readError)
		}
	case <-time.After(time.Second):
		t.Fatal("a line read waiting on an empty terminal was not cancelled")
	}
}
