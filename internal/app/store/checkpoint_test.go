package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func TestACheckpointRestoresTheJournalAndKeepsTheWriterLockedAndUsable(t *testing.T) {
	directory := t.TempDir()
	writer, err := Create(directory, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	writeCheckpointTurn(t, writer, "kept")
	restore, err := writer.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	writeCheckpointTurn(t, writer, "discarded")
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(directory, writer.Name()); err == nil {
		t.Fatal("undo released the session lock")
	}
	if err := restore(); err == nil {
		t.Fatal("a checkpoint could be restored twice")
	}
	stored, err := Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if stored.HasIncompleteTurn || len(stored.Items) != 1 || len(stored.Events) != 1 || stored.Events[0].Text != "kept" {
		t.Fatalf("wrong restored state: %+v", stored)
	}
	metadata, err := session.ReadMeta(directory, writer.Name())
	if err != nil || metadata.Messages != 1 {
		t.Fatalf("wrong restored listing: %+v, %v", metadata, err)
	}
	root, err := os.OpenRoot(filepath.Join(directory, writer.Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	transcript, err := root.ReadFile("chat.md")
	if err != nil || strings.Contains(string(transcript), "discarded") {
		t.Fatalf("wrong restored transcript: %s, %v", transcript, err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, writer.Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "undo") {
			t.Fatalf("undo left a copy of the journal: %s", entry.Name())
		}
	}
	writeCheckpointTurn(t, writer, "replacement")
	stored, err = Read(directory, writer.Name())
	if err != nil || len(stored.Items) != 2 || stored.Events[1].Text != "replacement" {
		t.Fatalf("writer failed after undo: %+v, %v", stored, err)
	}
}

func TestACheckpointCanRestoreTheFirstTurnToAnEmptySession(t *testing.T) {
	directory := t.TempDir()
	writer, err := Create(directory, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	restore, err := writer.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	writeCheckpointTurn(t, writer, "mistake")
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	stored, err := Read(directory, writer.Name())
	if err != nil || len(stored.Events) != 0 || len(stored.Items) != 0 || !stored.CanResume() {
		t.Fatalf("first turn was not removed: %+v, %v", stored, err)
	}
}

func writeCheckpointTurn(t *testing.T, writer *Writer, text string) {
	t.Helper()
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: text}); err != nil {
		t.Fatal(err)
	}
	item, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(item); err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		t.Fatal(err)
	}
}
