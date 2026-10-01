package notification_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/notification"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/ask"
)

func neverFocused() bool { return false }

func fakeNotifySend(t *testing.T) string {
	t.Helper()

	bin := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "arguments")
	fixture := "#!/bin/bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > \"$NOTIFY_CAPTURE\"\n"
	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), []byte(fixture), 0o700); err != nil {
		t.Fatalf("could not write fake notify-send: %v", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("NOTIFY_CAPTURE", capturePath)

	return capturePath
}

func capturedArguments(t *testing.T, capturePath string) []string {
	t.Helper()

	//nolint:gosec // the path is a test fixture below t.TempDir
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("could not read captured arguments: %v", err)
	}

	return strings.Split(strings.TrimSuffix(string(captured), "\n"), "\n")
}

func TestTurnErrorNotificationNamesTheWorkspaceAndShowsTheFailure(t *testing.T) {
	capturePath := fakeNotifySend(t)

	if err := notification.SendTurnError(
		t.Context(),
		nil,
		neverFocused,
		work.At("/workspace/io"),
		errors.New("access denied"),
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := capturedArguments(t, capturePath)
	want := []string{
		"--icon=dialog-error",
		"--app-name=oh",
		"--",
		"oh — io",
		"access denied",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got arguments %q, want %q", got, want)
	}
}

func TestQuestionNotificationNamesTheWorkspaceAndAsksTheQuestion(t *testing.T) {
	capturePath := fakeNotifySend(t)

	question := ask.Confirmation{
		Label:  "Run this command in the sandbox with host networking?",
		Detail: "curl example.com",
	}.Question()

	if _, err := notification.SendQuestion(
		t.Context(),
		nil,
		neverFocused,
		work.At("/workspace/io"),
		question,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := capturedArguments(t, capturePath)
	want := []string{
		"--icon=dialog-question",
		"--app-name=oh",
		"--expire-time=0",
		"--print-id",
		"--",
		"oh — io",
		"Run this command in the sandbox with host networking?",
		"curl example.com",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got arguments %q, want %q", got, want)
	}
}

func TestQuestionNotificationShowsTheFirstNamedField(t *testing.T) {
	capturePath := fakeNotifySend(t)
	question := ask.Confirmation{
		Label: "Run the commit tool?",
		Fields: []ask.Field{
			{Name: "patch", Value: "/tmp/layout.patch"},
			{Name: "message", Value: "Align header controls consistently"},
		},
	}.Question()

	if _, err := notification.SendQuestion(
		t.Context(),
		nil,
		neverFocused,
		work.At("/workspace/io"),
		question,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := capturedArguments(t, capturePath)
	if message := strings.Join(got[6:], "\n"); message != "Run the commit tool?\npatch: /tmp/layout.patch …" {
		t.Errorf("got message %q", message)
	}
}

func TestQuestionNotificationShortensWhatItCannotShow(t *testing.T) {
	for name, test := range map[string]struct {
		detail string
		want   string
	}{
		"nothing to show":  {detail: "", want: "Continue?"},
		"blank detail":     {detail: "  \n ", want: "Continue?"},
		"one step":         {detail: "make test", want: "Continue?\nmake test"},
		"further steps":    {detail: "make test\nmake install", want: "Continue?\nmake test …"},
		"a very long step": {detail: strings.Repeat("ab", 60), want: "Continue?\n" + strings.Repeat("ab", 39) + "a…"},
	} {
		t.Run(name, func(t *testing.T) {
			capturePath := fakeNotifySend(t)

			question := ask.Confirmation{Label: "Continue?", Detail: test.detail}.Question()
			if _, err := notification.SendQuestion(
				t.Context(),
				nil,
				neverFocused,
				work.At("/workspace/io"),
				question,
			); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := capturedArguments(t, capturePath)
			if message := strings.Join(got[6:], "\n"); message != test.want {
				t.Errorf("got message %q, want %q", message, test.want)
			}
		})
	}
}

func fakeSessionBus(t *testing.T, gdbusBody string) string {
	t.Helper()

	bin := t.TempDir()
	closedPath := filepath.Join(t.TempDir(), "closed")
	fixtures := map[string]string{
		"notify-send": "#!/bin/bash\nset -euo pipefail\nprintf 42\n",
		"gdbus":       "#!/bin/bash\nset -euo pipefail\n" + gdbusBody,
	}
	for name, fixture := range fixtures {
		//nolint:gosec // an executable test fixture
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fixture), 0o700); err != nil {
			t.Fatalf("could not write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("CLOSED_CAPTURE", closedPath)

	return closedPath
}

const recordClosed = "printf '%s\\n' \"${!#}\" >> \"$CLOSED_CAPTURE\"\n"

func closedNotifications(t *testing.T, closedPath string) []string {
	t.Helper()

	//nolint:gosec // the path is a test fixture below t.TempDir
	closed, err := os.ReadFile(closedPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("could not read closed notifications: %v", err)
	}

	return strings.Split(strings.TrimSuffix(string(closed), "\n"), "\n")
}

func standingQuestion() ask.Question {
	return ask.Confirmation{Label: "Continue?"}.Question()
}

func TestEveryStandingQuestionIsWithdrawnAtTheEnd(t *testing.T) {
	closedPath := fakeSessionBus(t, recordClosed)
	questions := notification.NewQuestions(nil, neverFocused, work.At("/workspace/io"))

	questions.Announce(standingQuestion())
	questions.WithdrawAll(time.Minute)

	if got := closedNotifications(t, closedPath); !slices.Equal(got, []string{"42"}) {
		t.Errorf("got closed notifications %q, want the one standing", got)
	}
}

func TestASettledQuestionIsWithdrawnOnlyOnce(t *testing.T) {
	closedPath := fakeSessionBus(t, recordClosed)
	questions := notification.NewQuestions(nil, neverFocused, work.At("/workspace/io"))

	withdraw := questions.Announce(standingQuestion())
	withdraw()
	withdraw()
	questions.WithdrawAll(time.Minute)

	if got := closedNotifications(t, closedPath); !slices.Equal(got, []string{"42"}) {
		t.Errorf("got closed notifications %q, want it closed once", got)
	}
}

func TestWithdrawingAtTheEndGivesUpOnASulkingDaemon(t *testing.T) {
	fakeSessionBus(t, "exec /bin/sleep 5\n")
	questions := notification.NewQuestions(nil, neverFocused, work.At("/workspace/io"))

	questions.Announce(standingQuestion())

	startedAt := time.Now()
	questions.WithdrawAll(100 * time.Millisecond)
	if took := time.Since(startedAt); took > 2*time.Second {
		t.Errorf("waited %s for the daemon, want the grace alone", took)
	}
}

func TestAQuestionAskedInFocusHasNothingToWithdraw(t *testing.T) {
	closedPath := fakeSessionBus(t, recordClosed)
	questions := notification.NewQuestions(nil, func() bool { return true }, work.At("/workspace/io"))

	questions.Announce(standingQuestion())
	questions.WithdrawAll(time.Minute)

	if got := closedNotifications(t, closedPath); len(got) != 0 {
		t.Errorf("got closed notifications %q, want none", got)
	}
}
