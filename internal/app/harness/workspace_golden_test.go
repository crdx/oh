package harness

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestGoldenTheTerminalReceivesTheWorkspaceOnStartupAndResume(t *testing.T) {
	compareWithGolden(t, "terminal-workspace", ".txt", map[string]func() string{
		"startup and resume": func() string {
			rig := newInteractiveRig(t, "First answer.")
			hostname, _ := os.Hostname()
			address := url.URL{Scheme: "file", Host: hostname, Path: rig.workspace}
			report := "\x1b]7;" + address.String() + "\x1b\\"

			fresh := rig.start("--yolo", "-m", "opencode-go/fake", "first question")
			fresh.waitFor("First answer.")
			fresh.quit()
			if count := strings.Count(fresh.String(), report); count != 1 {
				t.Fatalf("startup sent %d workspace reports, want one: %q", count, fresh.String())
			}

			stored := rig.storedSessions()
			if len(stored) != 1 {
				t.Fatalf("got %d sessions, want one to resume", len(stored))
			}
			resumed := rig.start("-r", stored[0].Name)
			resumed.waitFor("First answer.")
			resumed.quit()
			if count := strings.Count(resumed.String(), report); count != 1 {
				t.Fatalf("resume sent %d workspace reports, want one: %q", count, resumed.String())
			}

			printed := runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "-r", stored[0].Name)
			if strings.Contains(printed, "\x1b]7;") {
				t.Fatalf("redirected output contains a workspace report: %q", printed)
			}

			return fmt.Sprintf("startup: %q\nresume: %q\nredirected output: no workspace report\n", "\x1b]7;file://<host>/workspace\x1b\\", "\x1b]7;file://<host>/workspace\x1b\\")
		},
	})
}
