package harness

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
)

func TestTurnUndoPreservesTheDraftAfterTheRecalledMessage(t *testing.T) {
	for _, stage := range []string{"running", "completed"} {
		for name, draft := range map[string]string{
			"empty":     "",
			"single":    "another detail",
			"multiline": "extra café\nnext line",
			"spaces":    "  \n ",
		} {
			t.Run(stage+"/"+name, func(t *testing.T) {
				self, provider, inputLine, history, _ := undoFixture(t)
				if stage == "completed" {
					self.finish()
				}
				inputLine.SetText(draft)
				self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
				if stage == "running" {
					self.finish()
				}
				want := "a mistaken message"
				if draft != "" {
					want += agent.InterjectionSeparator + draft
				}
				if inputLine.Text() != want || inputLine.Cursor() != len([]rune(want)) {
					t.Fatalf("got draft %q at %d, want %q at its end", inputLine.Text(), inputLine.Cursor(), want)
				}
				if self.currentTurn.Running() || len(provider.items) != 0 {
					t.Fatal("undo sent the combined draft or retained provider history")
				}
			})
		}
	}
}

func TestTurnUndoUsesTheDraftPresentWhenTheStoppedTurnFinishes(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	inputLine.SetText("first draft")
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	inputLine.SetText("latest draft")
	self.finish()
	if inputLine.Text() != "a mistaken message\n\nlatest draft" {
		t.Fatalf("undo lost the latest draft: %q", inputLine.Text())
	}
}

func TestRepeatedUndoCollectsTheQueueAndTurnWithoutLosingTheDraft(t *testing.T) {
	self, provider, inputLine, history, _ := undoFixture(t)
	self.currentTurn.Interject("older queued message")
	self.currentTurn.Interject("newer queued message")
	inputLine.SetText("existing draft")
	for range 3 {
		self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	}
	self.finish()
	want := "a mistaken message\n\nolder queued message\n\nnewer queued message\n\nexisting draft"
	for range 5 {
		self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	}
	if inputLine.Text() != want || len(provider.items) != 0 || self.currentTurn.Running() {
		t.Fatalf("repeated undo lost or sent text: %q", inputLine.Text())
	}
	if self.undoHint(replayColumns) != "nothing to undo" {
		t.Fatalf("unexpected empty undo status: %q", self.undoHint(replayColumns))
	}
}

func TestShortcutHelpOffersTurnUndoWithADraft(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	inputLine.SetText("existing draft")
	self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
	isOffered := false
	for _, shortcut := range self.keyboardShortcuts(inputLine) {
		if shortcut.keys == "^Z" && strings.Contains(shortcut.action, "last turn") {
			isOffered = true
		}
	}
	if !isOffered {
		t.Fatal("help hid turn undo because there was a draft")
	}
	self.finish()
}

func TestTurnUndoWithADraftThroughTheBinaryKeepsItUnsent(t *testing.T) {
	rig, endpoint, requested, release := newHeldResponseRig(t, "The revised answer.")
	conversation := rig.start("--yolo", "-m", "opencode-go/fake", "original prompt")
	select {
	case <-requested:
	case <-time.After(interactiveDeadline):
		t.Fatal("the provider request never arrived")
	}
	conversation.typeAndSettle("and the extra detail")
	previousDraws := conversation.Count("original prompt")
	conversation.typeText(pressCtrlZ)
	conversation.waitForCount("original prompt", previousDraws+1)
	conversation.requireShown("original prompt")
	conversation.requireShown("and the extra detail")
	release()
	if len(endpoint.Requests()) != 1 {
		t.Fatal("undo sent the combined draft")
	}
	stored := rig.storedSessions()
	if len(stored) != 1 || len(stored[0].Items) != 0 {
		t.Fatal("undo retained the discarded provider history")
	}
	conversation.typeText(pressEnter)
	conversation.waitFor("The revised answer.")
	requests := endpoint.Requests()
	if len(requests) != 2 {
		t.Fatalf("explicit send made %d requests, want two in total", len(requests))
	}
	isCombined := false
	for _, entry := range requests[1].Input {
		if entry.Role == "user" && entry.Content == "original prompt\n\nand the extra detail" {
			isCombined = true
		}
	}
	if !isCombined {
		t.Fatalf("the combined draft did not reach the provider: %+v", requests[1].Input)
	}
	conversation.quit()
}

func TestGoldenTurnUndoPreservesTheDraftAndUsesLowercaseStatus(t *testing.T) {
	passes := map[string]func() string{}
	for _, stage := range []string{"1 undoing", "2 restored", "3 nothing left"} {
		passes[stage] = func() string {
			self, _, inputLine, history, drawn := undoFixture(t)
			inputLine.SetText("another detail\nkeep this too")
			self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			if stage != "1 undoing" {
				self.finish()
			}
			if stage == "3 nothing left" {
				self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			}
			result := drawn.String()
			if self.currentTurn.Running() {
				self.finish()
			}
			return result
		}
	}
	compareWithGolden(t, "undo-draft", ".ansi", passes)
	compareWithGolden(t, "undo-draft", ".screen", shownPasses(t, passes))
}
