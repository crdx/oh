package sessions

import (
	"errors"
	"os"
	"strings"
	"testing"

	"crdx.org/duckopt/v2"

	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func TestDeleteOptionParsesBothSpellings(t *testing.T) {
	for _, spelling := range []string{"-d", "--delete"} {
		t.Run(spelling, func(t *testing.T) {
			original := os.Args
			os.Args = []string{"oh", "--ctl", "sessions", spelling, "tame-impala"}
			defer func() { os.Args = original }()

			options, err := duckopt.Bind[inputOpts](Usage, "$0")
			if err != nil {
				t.Fatal(err)
			}
			if !options.Delete || options.Label != "tame-impala" || options.Filter != "" {
				t.Fatalf("unexpected options: %+v", options)
			}
		})
	}
}

func TestDeletingASessionByExactLabel(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())
	directory := location.GetSessionsDir()
	storedSessionNamed(t, directory, "tame-impala")
	storedSessionNamed(t, directory, "tame-otter")

	var screen, failure strings.Builder
	if err := run(&inputOpts{Delete: true, Label: "tame-impala"}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}
	if got := screen.String(); got != "deleted session \"tame-impala\"\n" {
		t.Errorf("unexpected output: %q", got)
	}
	if session.Exists(directory, "tame-impala") || !session.Exists(directory, "tame-otter") {
		t.Error("deletion did not target exactly one session")
	}
}

func TestDeletingAnArchiveByExactLabel(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())
	directory := location.GetSessionsDir()
	storedSessionNamed(t, directory, "tame-impala")
	if err := session.Archive(directory, "tame-impala"); err != nil {
		t.Fatal(err)
	}
	if err := run(&inputOpts{Delete: true, Label: "tame-impala"}, console.Output{Screen: &strings.Builder{}}); err != nil {
		t.Fatal(err)
	}
	if session.Exists(directory, "tame-impala") {
		t.Error("archive was not deleted")
	}
}

func TestDeletingAnUnknownSessionRefusesIt(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())
	if err := run(&inputOpts{Delete: true, Label: "missing-otter"}, console.Output{Screen: &strings.Builder{}}); err == nil || !strings.Contains(err.Error(), "no stored or archived session") {
		t.Errorf("expected a missing-session error, got %v", err)
	}
}

func TestDeletingARunningSessionRefusesIt(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())
	directory := location.GetSessionsDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	if err := run(&inputOpts{Delete: true, Label: writer.Name()}, console.Output{Screen: &strings.Builder{}}); !errors.Is(err, session.ErrInUse) {
		t.Errorf("expected the running session to be protected, got %v", err)
	}
	if !session.Exists(directory, writer.Name()) {
		t.Error("running session was removed")
	}
}
