package terminal

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/work"
)

func TestTheTerminalReportsItsWorkspaceAsAnEscapedFileURL(t *testing.T) {
	for _, directory := range []string{
		"/workspace/project",
		"/workspace/a space/日本語#100%",
		"/workspace/control\n\x1b]7;elsewhere\a",
	} {
		t.Run(directory, func(t *testing.T) {
			controller, terminal := ptytest.Open(t)
			screen := ptytest.Record(controller)
			managedTerminal := New(terminal, work.At(directory))
			restore := managedTerminal.Begin(caps.Read)
			defer restore()

			if !screen.WaitForCount("\x1b\\", 1, time.Second) {
				t.Fatalf("the terminal received no workspace report: %q", screen.String())
			}
			report, _, _ := strings.Cut(strings.TrimPrefix(screen.String(), "\x1b]7;"), "\x1b\\")
			address, err := url.Parse(report)
			if err != nil {
				t.Fatal(err)
			}
			if address.Scheme != "file" || address.Path != directory {
				t.Errorf("got workspace URL %q, want directory %q", report, directory)
			}
			if strings.ContainsAny(report, "\n\x1b\a") {
				t.Errorf("the workspace report contains terminal controls: %q", report)
			}
		})
	}
}

func TestTheTerminalDoesNotReportItsWorkspaceToRedirectedOutput(t *testing.T) {
	var output bytes.Buffer
	managedTerminal := New(&output, work.At("/workspace/project"))
	restore := managedTerminal.Begin(caps.Read)
	restore()
	if output.Len() != 0 {
		t.Errorf("the terminal wrote escape sequences to redirected output: %q", output.String())
	}
}
