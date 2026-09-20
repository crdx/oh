package notification_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
		Label:  "Run this command with host networking?",
		Detail: "curl example.com",
	}.Question()

	if err := notification.SendQuestion(
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
		"--",
		"oh — io",
		"Run this command with host networking?",
		"curl example.com",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got arguments %q, want %q", got, want)
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
			if err := notification.SendQuestion(
				t.Context(),
				nil,
				neverFocused,
				work.At("/workspace/io"),
				question,
			); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := capturedArguments(t, capturePath)
			if message := strings.Join(got[4:], "\n"); message != test.want {
				t.Errorf("got message %q, want %q", message, test.want)
			}
		})
	}
}
