package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRejectsASessionWithoutAHead(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "broken-toad"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken-toad", "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(directory, "broken-toad"); err == nil {
		t.Error("expected an empty session to be rejected")
	}
}

func TestCompletingATurnSyncsTheJournalBeforeWritingMetadata(t *testing.T) {
	journalReader, journalWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journalReader.Close() }()

	writer := &Writer{
		file:        journalWriter,
		directory:   t.TempDir(),
		listingMeta: Meta{Name: "missing-directory"},
	}
	defer func() { _ = writer.Close() }()

	err = writer.CompleteTurn(TurnSummary{})
	if err == nil || !strings.Contains(err.Error(), "sync session journal") {
		t.Fatalf("got %v, want the pipe's sync failure", err)
	}
}

func TestSessionNamesCannotEscapeTheSessionDirectory(t *testing.T) {
	if _, err := Read(t.TempDir(), "../somewhere-else"); err == nil {
		t.Error("expected a path-like session name to be rejected")
	}
}

func TestEveryBitOfAnIDSurvivesBeingWritten(t *testing.T) {
	var least, most [16]byte
	for i := range most {
		most[i] = 0xff
	}

	tests := map[[16]byte]string{
		least: "0000000000000000000000",
		most:  "7n42DGM5Tflk9n8mt7Fhc7",
	}
	for id, want := range tests {
		if got := encode(id); got != want {
			t.Errorf("encode(%x) = %q, want %q", id, got, want)
		}
	}
}
