package regenerate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/ctl/console"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

const (
	goldenName      = "stored-session"
	goldenDirectory = "/state/sessions"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", helpText(usage))
}

func TestGoldenWhatIsWrittenAgainMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()
	name := storedSession(t, directory)

	var screen, failure strings.Builder
	if err := run(directory, nil, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "report.txt", report(screen.String(), failure.String(), name))
}

func TestGoldenAnArchivedSessionWithoutATranscriptMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()
	name := storedSession(t, directory)
	if err := os.Remove(filepath.Join(directory, name, "chat.md")); err != nil {
		t.Fatal(err)
	}
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	if err := run(directory, nil, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "archived.txt", report(screen.String(), failure.String(), name))
}

func TestGoldenWhatCannotBeWrittenAgainMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()
	name := storedSession(t, directory)

	var screen, failure strings.Builder
	err := run(directory, []string{name, "tame-impala"}, console.Output{Screen: &screen, Failure: &failure})
	if err == nil {
		t.Fatal("expected the missing session to be reported as a failure")
	}

	assertGolden(t, "failure.txt", strings.Join([]string{
		report(screen.String(), failure.String(), name),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestGoldenAnEmptyDirectoryMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()

	var screen, failure strings.Builder
	err := run(directory, nil, console.Output{Screen: &screen, Failure: &failure})
	if err == nil {
		t.Fatal("expected an empty directory to be reported")
	}

	assertGolden(t, "nothing-stored.txt", strings.Join([]string{
		report(screen.String(), failure.String(), directory),
		"=== error ===\n", strings.ReplaceAll(err.Error(), directory, goldenDirectory), "\n",
	}, ""))
}

func helpText(usage string) string {
	return strings.ReplaceAll(usage, "$0", "oh")
}

func storedSession(t *testing.T, directory string) string {
	t.Helper()

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir(), Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return writer.Name()
}

func report(screen string, failure string, named string) string {
	text := strings.Join([]string{
		"=== screen ===\n", screen,
		"=== failure ===\n", failure,
	}, "")

	return strings.ReplaceAll(text, named, goldenName)
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
