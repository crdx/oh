package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareInitialFilesCopiesFilesIntoTheScratchDirectoryAndListsThem(t *testing.T) {
	sourceDirectory := t.TempDir()
	sourcePaths := []string{
		filepath.Join(sourceDirectory, "brief.md"),
		filepath.Join(sourceDirectory, "notes.txt"),
	}
	for i, sourcePath := range sourcePaths {
		if err := os.WriteFile(sourcePath, fmt.Appendf(nil, "file %d\n", i), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scratchDirectory := t.TempDir()

	files := make([]InitialFile, len(sourcePaths))
	for i, sourcePath := range sourcePaths {
		files[i] = InitialFile{SourcePath: sourcePath}
	}

	message, err := PrepareInitialFiles(files, scratchDirectory)
	if err != nil {
		t.Fatal(err)
	}

	for i, name := range []string{"brief.md", "notes.txt"} {
		copied, err := os.ReadFile(filepath.Join(scratchDirectory, name)) //nolint:gosec // the test's own path
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("file %d\n", i)
		if string(copied) != want {
			t.Errorf("got copied content %q, want %q", copied, want)
		}
	}

	wantMessage := fmt.Sprintf(
		"%s\n\n- %s\n- %s",
		messageIntroduction,
		filepath.Join(scratchDirectory, "brief.md"),
		filepath.Join(scratchDirectory, "notes.txt"),
	)
	if message != wantMessage {
		t.Errorf("got message %q, want %q", message, wantMessage)
	}
}

func TestPrepareInitialFilesReportsAnUnreadableFile(t *testing.T) {
	missing := []InitialFile{{SourcePath: filepath.Join(t.TempDir(), "missing.md")}}
	_, err := PrepareInitialFiles(missing, t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestPrepareInitialFilesUsesTheDisplayNameWhenGiven(t *testing.T) {
	sourceDirectory := t.TempDir()
	sourcePath := filepath.Join(sourceDirectory, "chat.md")
	if err := os.WriteFile(sourcePath, []byte("transcript"), 0o600); err != nil {
		t.Fatal(err)
	}
	scratchDirectory := t.TempDir()

	message, err := PrepareInitialFiles([]InitialFile{
		{SourcePath: sourcePath, DisplayName: "oaken-elephant.chat.md"},
	}, scratchDirectory)
	if err != nil {
		t.Fatal(err)
	}

	copied, err := os.ReadFile(filepath.Join(scratchDirectory, "oaken-elephant.chat.md")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != "transcript" {
		t.Errorf("got copied content %q, want %q", copied, "transcript")
	}

	wantMessage := fmt.Sprintf(
		"%s\n\n- %s",
		messageIntroduction,
		filepath.Join(scratchDirectory, "oaken-elephant.chat.md"),
	)
	if message != wantMessage {
		t.Errorf("got message %q, want %q", message, wantMessage)
	}
}
