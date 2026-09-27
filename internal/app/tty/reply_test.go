package tty

import (
	"io"
	"regexp"
	"slices"
	"testing"
	"time"

	"golang.org/x/term"

	"crdx.org/oh/internal/app/ptytest"
)

var testReply = regexp.MustCompile(`\x1b\[[0-9]+;[0-9]+R`)

func hasOneReply(replies []string) bool {
	return len(replies) >= 1
}

func TestKeysTypedAroundAReplyAreKeptForTheNextReader(t *testing.T) {
	t.Cleanup(func() { takeHeld(make([]byte, maximumRead)) })

	controller, terminal := ptytest.Open(t)
	state, err := term.MakeRaw(int(terminal.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Restore(int(terminal.Fd()), state) })

	if _, err := controller.WriteString("early\x1b[4;1Rlate"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	replies := ReadReplies(terminal, testReply, hasOneReply)
	if !slices.Equal(replies, []string{"\x1b[4;1R"}) {
		t.Errorf("got replies %q, want the one position", replies)
	}

	reader := NewReader(terminal)
	t.Cleanup(reader.Close)
	kept := make([]byte, len("earlylate"))
	if _, err := io.ReadFull(reader, kept); err != nil {
		t.Fatal(err)
	}
	if string(kept) != "earlylate" {
		t.Errorf("the next reader got %q, want what was typed around the reply", kept)
	}
}

func TestAReplyThatNeverComesKeepsEverythingTyped(t *testing.T) {
	t.Cleanup(func() { takeHeld(make([]byte, maximumRead)) })

	controller, terminal := ptytest.Open(t)
	state, err := term.MakeRaw(int(terminal.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Restore(int(terminal.Fd()), state) })

	if _, err := controller.WriteString("typed"); err != nil {
		t.Fatal(err)
	}

	if replies := ReadReplies(terminal, testReply, hasOneReply); len(replies) != 0 {
		t.Errorf("got replies %q from a terminal that sent none", replies)
	}

	kept := make([]byte, maximumRead)
	if count := takeHeld(kept); string(kept[:count]) != "typed" {
		t.Errorf("kept %q, want everything typed", kept[:count])
	}
}
