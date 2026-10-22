package notification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/stop"
)

func TestKittyNotificationWritesEscapeCodeAndMapsIcon(t *testing.T) {
	const escapeCode = "\x1b]99;i=1:d=0;VGl0bGU=\x1b\\\x1b]99;i=1;\x1b\\"

	bin := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "arguments")
	fixture := "#!/bin/bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > \"$NOTIFY_CAPTURE\"\nprintf '%s' \"$NOTIFY_ESCAPE\"\n"
	//nolint:gosec // This executable is a test fixture.
	if err := os.WriteFile(filepath.Join(bin, "kitten"), []byte(fixture), 0o700); err != nil {
		t.Fatalf("could not write fake kitten: %v", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "1")
	t.Setenv("NOTIFY_CAPTURE", capturePath)
	t.Setenv("NOTIFY_ESCAPE", escapeCode)

	var written []string
	notice, err := Send(t.Context(), func(escape string) bool {
		written = append(written, escape)
		return true
	}, Args{Title: "Build", Message: "The build is finished", Icon: "error"}, ExpiresAsUsual)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !notice.IsSent() {
		t.Fatal("the notification was not sent")
	}

	//nolint:gosec // The path is inside the test directory.
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("could not read captured arguments: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(captured), "\n"), "\n")
	want := []string{
		"notify",
		"--only-print-escape-code",
		"--icon=dialog-error",
		"--app-name=oh",
		"--",
		"Build",
		"The build is finished",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got arguments %q, want %q", got, want)
	}
	if !slices.Equal(written, []string{escapeCode}) {
		t.Errorf("got escape codes %q, want the whole code", written)
	}
}

func TestNotificationTextCannotBecomeAnOption(t *testing.T) {
	for name, kittyWindow := range map[string]string{"kitty": "1", "notify-send": ""} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("KITTY_WINDOW_ID", kittyWindow)
			process, _ := command(
				t.Context(),
				"--wait <io>",
				"--urgency=critical & finish",
				"dialog-information",
				ExpiresAsUsual,
				"",
			)

			got := process.Args[len(process.Args)-3:]
			want := []string{"--", "--wait <io>", "--urgency=critical & finish"}
			if !slices.Equal(got, want) {
				t.Errorf("got trailing arguments %q, want %q", got, want)
			}
		})
	}
}

func TestNotificationRequiresKnownNonBlankFields(t *testing.T) {
	for name, arguments := range map[string]Args{
		"title":   {Title: " ", Message: "hello", Icon: "info"},
		"message": {Title: "Greeting", Message: " ", Icon: "info"},
		"icon":    {Title: "Greeting", Message: "hello", Icon: "good"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(arguments); err == nil {
				t.Fatal("invalid notification was accepted")
			}
		})
	}
}

func TestCancelledNotificationStopsNotifySend(t *testing.T) {
	bin := t.TempDir()
	fixture := "#!/bin/bash\nset -euo pipefail\nexec /bin/sleep 60\n"
	//nolint:gosec // This executable is a test fixture.
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), []byte(fixture), 0o700); err != nil {
		t.Fatalf("could not write fake notify-send: %v", err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("KITTY_WINDOW_ID", "")

	ctx, cancel := context.WithCancelCause(t.Context())
	time.AfterFunc(100*time.Millisecond, func() { cancel(stop.Because("the user sent another message")) })

	startedAt := time.Now()
	_, err := Send(ctx, nil, Args{Title: "Greeting", Message: "hello", Icon: "info"}, ExpiresAsUsual)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation, got %v", err)
	}
	if want := "the notification stopped because the user sent another message"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
	if took := time.Since(startedAt); took > 2*time.Second {
		t.Errorf("expected notify-send to stop promptly, took %s", took)
	}
}
