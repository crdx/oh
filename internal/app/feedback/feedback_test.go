package feedback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
)

const testDismissAfter = 4 * time.Second

func TestOnlyCommandAndConfirmationCanBeDismissed(t *testing.T) {
	cases := map[Source]bool{
		System:       false,
		Command:      true,
		Config:       false,
		Confirmation: true,
	}

	for source, want := range cases {
		if got := source.CanBeDismissed(); got != want {
			t.Errorf("%v.CanBeDismissed() = %v, want %v", source, got, want)
		}
	}
}

func TestDismissingReportsWhetherItTookAMessageAway(t *testing.T) {
	var self State

	if self.Dismiss() {
		t.Error("an empty state reported a dismissal")
	}

	self.Show(System, Message{Text: "chat.md recording disabled", Status: agent.ErrorStatus}, time.Now())
	if self.Dismiss() {
		t.Error("a message nothing may dismiss reported a dismissal")
	}
	if self.IsEmpty() {
		t.Error("a message nothing may dismiss was taken away")
	}

	self.Show(Command, Message{Text: "Command not found: /unknown", Status: agent.ErrorStatus}, time.Now())
	if !self.Dismiss() {
		t.Error("a dismissable message reported no dismissal")
	}
	if !self.IsEmpty() {
		t.Error("a dismissable message stayed on screen")
	}
}

func TestAMessageWithNoDismissAfterNeverSchedulesARefresh(t *testing.T) {
	var self State

	self.Show(System, Message{Text: "done", Status: agent.SuccessStatus}, time.Now())

	if got := self.NextRefresh(time.Now()); !got.IsZero() {
		t.Errorf("next refresh = %s, want none scheduled", got)
	}
}

func TestAMessageWithItsOwnStyleIsNotOverpainted(t *testing.T) {
	var self State
	text := "\x1b[31mred\x1b[0m plain"
	self.Show(Command, Message{Text: text, Status: agent.InfoStatus, HasOwnStyle: true}, time.Now())

	got := strutil.VisibleEscapes(renderedText(self.Render(80, time.Now()))) + "\n"
	want, err := os.ReadFile(filepath.Join("testdata", "own-style.ansi"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAMessageWithNoDismissAfterCarriesNoCountdown(t *testing.T) {
	var self State

	self.Show(System, Message{Text: "done", Status: agent.SuccessStatus}, time.Now())

	if got := renderedText(self.Render(80, time.Now())); strings.Contains(got, "dismissing") {
		t.Errorf("rendered = %q, want no countdown", got)
	}
}

func TestAMessageClearsItselfOnceItsDismissAfterElapses(t *testing.T) {
	var self State

	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.Show(Confirmation, Message{Text: "done", Status: agent.SuccessStatus, DismissAfter: testDismissAfter}, base)

	dismissingAt := base.Add(testDismissAfter)

	self.ClearExpired(dismissingAt.Add(-time.Nanosecond))
	if self.IsEmpty() {
		t.Error("feedback was cleared before its delay elapsed")
	}

	self.ClearExpired(dismissingAt)
	if !self.IsEmpty() {
		t.Error("feedback was not cleared once its delay elapsed")
	}
}

func TestAMessageTicksOnceASecondUntilItDismisses(t *testing.T) {
	var self State

	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.Show(Confirmation, Message{Text: "done", Status: agent.SuccessStatus, DismissAfter: testDismissAfter}, base)

	dismissingAt := base.Add(testDismissAfter)

	if got, want := self.NextRefresh(base), base.Add(time.Second); !got.Equal(want) {
		t.Errorf("next refresh = %s, want the next countdown tick %s", got, want)
	}

	if got := self.NextRefresh(dismissingAt.Add(-500 * time.Millisecond)); !got.Equal(dismissingAt) {
		t.Errorf("next refresh = %s, want the dismissal time once within the last second", got)
	}
}

func TestAMessageCountsDownInWholeSeconds(t *testing.T) {
	var self State

	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.Show(Confirmation, Message{Text: "done", Status: agent.SuccessStatus, DismissAfter: testDismissAfter}, base)

	if got, want := renderedText(self.Render(80, base)), "(dismissing in 4s)"; !strings.Contains(got, want) {
		t.Errorf("rendered = %q, want it to contain %q", got, want)
	}

	afterThreeAndAHalfSeconds := base.Add(3500 * time.Millisecond)
	if got, want := renderedText(self.Render(80, afterThreeAndAHalfSeconds)), "(dismissing in 1s)"; !strings.Contains(got, want) {
		t.Errorf("rendered = %q, want it to contain %q", got, want)
	}
}

func renderedText(rows []string) string {
	return strings.Join(rows, "\n")
}
