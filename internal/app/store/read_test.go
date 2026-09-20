package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRejectsASessionWithoutAHead(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken", "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(directory, "broken"); err == nil {
		t.Error("expected an empty session to be rejected")
	}
}

func TestSessionIDsCannotEscapeTheSessionDirectory(t *testing.T) {
	if _, err := Read(t.TempDir(), "../somewhere-else"); err == nil {
		t.Error("expected a path-like session ID to be rejected")
	}
}

func TestASessionCanResumeOnlyWithoutAnIncompleteTurn(t *testing.T) {
	for name, test := range map[string]struct {
		hasIncompleteTurn bool
		canResume         bool
	}{
		"empty":            {canResume: true},
		"completed":        {canResume: true},
		"still incomplete": {hasIncompleteTurn: true},
	} {
		t.Run(name, func(t *testing.T) {
			storedSession := &Session{HasIncompleteTurn: test.hasIncompleteTurn}
			if got := storedSession.CanResume(); got != test.canResume {
				t.Errorf("CanResume() = %v, want %v", got, test.canResume)
			}
		})
	}
}
