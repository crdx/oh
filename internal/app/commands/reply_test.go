package commands

import (
	"errors"
	"strings"
	"testing"
)

func TestAReplyQuotesEveryLineOfTheMessage(t *testing.T) {
	for message, want := range map[string]string{
		"One line.":                              "> One line.\n\n",
		"First.\n\nSecond.\n":                    "> First.\n>\n> Second.\n\n",
		"> quoted\n- item\n\n```go\nx := 1\n```": "> > quoted\n> - item\n>\n> ```go\n> x := 1\n> ```\n\n",
		"  indented\n\n\n":                       ">   indented\n\n",
	} {
		if got := QuoteReply(message); got != want {
			t.Errorf("QuoteReply(%q) got %q, want %q", message, got, want)
		}
	}
}

func TestReplyHandsTheQuotedLastMessageToTheEditor(t *testing.T) {
	var drafts []string
	commands := newCommandRegistry(t, commandEnvironment{
		session: commandSession{getLastMessage: func() (string, bool) { return "An answer.\n\nWith two paragraphs.", true }},
		replyInEditor: func(text string) error {
			drafts = append(drafts, text)
			return nil
		},
	})

	invocation, found := commands.Find("/reply")
	if !found {
		t.Fatal("expected /reply to be registered")
	}
	if err := invocation.Command.Run(newCommandTestContext(t), invocation.Arguments); err != nil {
		t.Fatal(err)
	}

	if want := "> An answer.\n>\n> With two paragraphs.\n\n"; len(drafts) != 1 || drafts[0] != want {
		t.Errorf("got drafts %q, want %q", drafts, want)
	}
}

func TestReplyRefusesWhenThereIsNothingToReplyTo(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		session: commandSession{getLastMessage: func() (string, bool) { return "", false }},
		replyInEditor: func(string) error {
			t.Error("the editor was opened with nothing to reply to")
			return nil
		},
	})

	invocation, found := commands.Find("/reply")
	if !found {
		t.Fatal("expected /reply to be registered")
	}
	err := invocation.Command.Run(newCommandTestContext(t), invocation.Arguments)
	if err == nil || !strings.Contains(err.Error(), "no model message has been received yet") {
		t.Errorf("got error %v, want the missing message named", err)
	}
}

func TestReplyTakesNoArguments(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		session:       commandSession{getLastMessage: func() (string, bool) { return "An answer.", true }},
		replyInEditor: func(string) error { return nil },
	})

	invocation, found := commands.Find("/reply now")
	if !found {
		t.Fatal("expected /reply to be registered")
	}
	err := invocation.Command.Run(newCommandTestContext(t), invocation.Arguments)
	if err == nil {
		t.Error("got no error for an argument /reply does not take")
	}
}

func TestReplyIsAbsentUnlessSomethingCanEditTheReply(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{})

	if _, found := commands.Find("/reply"); found {
		t.Error("/reply was registered with no way to edit the reply")
	}
}

func TestReplyPassesOnWhyTheEditorCouldNotOpen(t *testing.T) {
	refusal := errors.New("an editor is already open")
	commands := newCommandRegistry(t, commandEnvironment{
		session:       commandSession{getLastMessage: func() (string, bool) { return "An answer.", true }},
		replyInEditor: func(string) error { return refusal },
	})

	invocation, _ := commands.Find("/reply")
	if err := invocation.Command.Run(newCommandTestContext(t), invocation.Arguments); !errors.Is(err, refusal) {
		t.Errorf("got error %v, want %v", err, refusal)
	}
}
