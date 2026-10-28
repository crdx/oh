package harness

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

func TestGoldenRestoringTheOriginalConfigClearsOnlyItsReloadError(t *testing.T) {
	passes := map[string]func() string{}
	for _, stage := range []string{"invalid", "still invalid", "restored", "other error"} {
		passes[stage] = func() string {
			path := filepath.Join(t.TempDir(), "config.toml")
			const original = "[ui]\nreasoning = \"plain\"\n"
			writeLiveConfig(t, path, original)
			self := testConversation(t, &bytes.Buffer{})
			prepareLiveConfig(t, self, path)

			writeLiveConfig(t, path, "<<<<<<< HEAD\n")
			settleLiveConfig(t, self)
			if self.feedback.Message().Status != agent.ErrorStatus {
				t.Fatal("invalid config did not show an error")
			}
			if stage == "still invalid" {
				before := self.feedback.Message()
				if self.reloadConfig(nil) || self.feedback.Message() != before {
					t.Fatal("an unchanged invalid config changed its feedback")
				}
			}
			if stage == "other error" {
				self.showFeedback(feedback.Command, feedback.Message{Text: "Command not found: /unknown", Status: agent.ErrorStatus})
			}
			if stage == "restored" || stage == "other error" {
				writeLiveConfig(t, path, original)
				settleLiveConfig(t, self)
				if stage == "restored" && !self.feedback.IsEmpty() {
					t.Fatalf("restored config left feedback: %+v", self.feedback.Message())
				}
				if stage == "other error" && self.feedback.Message().Text != "Command not found: /unknown" {
					t.Fatal("restoring config dismissed another error")
				}
				if self.display.reasoningRendering != output.ReasoningPlain {
					t.Fatal("restoring config changed the live settings")
				}
				if self.reloadConfig(nil) {
					t.Fatal("unchanged valid config requested another repaint")
				}
			}

			var drawn strings.Builder
			self.screen = output.NewTerminalOfSize(&drawn, replayColumns, replayLines)
			message := self.feedback.Message()
			message.Text = strings.ReplaceAll(message.Text, path, "oh.toml")
			self.feedback.Show(feedback.Config, message, self.getNow())
			self.show(edit.NewInput(nil))
			return shown(t, drawn.String(), replayColumns)
		}
	}
	compareWithGolden(t, "config-reload-recovery", ".screen", passes)
}

func TestRestoringConfigAfterAWatchFailureClearsItsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "")
	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)
	self.reloadConfig(errors.New("watch stopped"))
	if self.feedback.IsEmpty() {
		t.Fatal("watch failure did not show an error")
	}
	if !self.reloadConfig(nil) || !self.feedback.IsEmpty() {
		t.Fatal("valid unchanged config did not clear the watch error")
	}
}

func TestAnInteractiveConfigRecoveryRemovesTheErrorWithoutAnotherEdit(t *testing.T) {
	rig := newInteractiveRig(t, "Ready for config changes.")
	path := filepath.Join(rig.workspace, "oh.toml")
	const original = "[ui]\nreasoning = \"plain\"\n"
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(original)
	session := rig.start("--yolo", "-m", "opencode-go/fake", "wait for config changes")
	session.waitFor("Ready for config changes.")
	write("<<<<<<< HEAD\n")
	session.waitFor("The configuration could not be reloaded")
	session.requireShown("The configuration could not be reloaded")
	before := session.Len()
	write(original)
	if !session.WaitForGrowth(before, interactiveDeadline) {
		t.Fatal("restoring the original config did not repaint the screen")
	}
	session.waitToSettle()
	session.requireHidden("The configuration could not be reloaded")
	session.requireHidden("Configuration reloaded automatically")
	session.requireShown("Ready for config changes.")
	session.quit()
}
