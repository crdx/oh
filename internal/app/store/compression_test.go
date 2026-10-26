package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func TestCompressionRejectionsPersistBeforeTheTurnFinishesWithOrWithoutWireRecording(t *testing.T) {
	for _, isWireOn := range []bool{false, true} {
		name := "without wire recording"
		if isWireOn {
			name = "with wire recording"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			log, err := store.Create(directory, store.Meta{Provider: "codex", Model: "fake", ShouldSkipWire: !isWireOn})
			if err != nil {
				t.Fatal(err)
			}
			observer, isCounting := log.Observer().(req.CompressionObserver)
			if !isCounting {
				t.Fatal("the session observer does not record compression rejections")
			}
			observer.CompressionRejected()
			if log.IsPersisted() {
				t.Fatal("a background diagnostic created an empty session")
			}
			if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
				t.Fatal(err)
			}
			observer.CompressionRejected()
			stored, err := store.Read(directory, log.Name())
			if err != nil {
				t.Fatal(err)
			}
			if len(stored.Events) != 2 || stored.Events[1].Kind != metrics.RequestCompressionRejected || !stored.HasIncompleteTurn {
				t.Fatalf("the unfinished turn did not retain its rejection: %+v", stored)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}

			root, err := os.OpenRoot(filepath.Join(directory, log.Name()))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			transcript, err := root.ReadFile("chat.md")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(transcript), string(metrics.RequestCompressionRejected)) || strings.Count(string(transcript), "## ") != 1 {
				t.Errorf("a diagnostic changed the conversation transcript: %s", transcript)
			}
			if _, err := root.Stat("debug/wire.http.zst"); isWireOn && err != nil || !isWireOn && !os.IsNotExist(err) {
				t.Errorf("wire recording state differs from %t: %v", isWireOn, err)
			}

			writer, err := session.Open(directory, log.Name())
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.AbandonTurn(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			stored, err = store.Read(directory, log.Name())
			if err != nil {
				t.Fatal(err)
			}
			if len(stored.Events) != 2 || stored.Events[1].Kind != metrics.RequestCompressionRejected || !stored.HasAbandonedTurn {
				t.Fatalf("abandoning the turn lost its rejection: %+v", stored)
			}
		})
	}
}
