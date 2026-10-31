package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestAStreamWithoutPassesIsTakenWhole(t *testing.T) {
	got, err := choosePass("one\ntwo\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "one\ntwo\n" {
		t.Errorf("got %q", got)
	}
}

func TestAPassIsTakenByItsLabel(t *testing.T) {
	contents := "=== first ===\none\n=== second ===\ntwo\nlines\n"

	got, err := choosePass(contents, "second")
	if err != nil {
		t.Fatal(err)
	}
	if got != "two\nlines" {
		t.Errorf("got %q", got)
	}
}

func TestAStreamOfOnePassNeedsNoLabel(t *testing.T) {
	got, err := choosePass("=== only ===\none\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "one" {
		t.Errorf("got %q", got)
	}
}

func TestSeveralPassesNeedOneNamed(t *testing.T) {
	_, err := choosePass("=== first ===\none\n=== second ===\ntwo\n", "")
	if err == nil || !strings.Contains(err.Error(), "first, second") {
		t.Errorf("got %v, want the passes listed", err)
	}
}

func TestAPassThatIsNotThereIsRefused(t *testing.T) {
	_, err := choosePass("=== first ===\none\n", "third")
	if err == nil || !strings.Contains(err.Error(), `no pass "third"`) {
		t.Errorf("got %v", err)
	}
}

func TestASizeMustBeAWholeNumberAboveZero(t *testing.T) {
	for _, value := range []string{"0", "-3", "wide", "1.5"} {
		if _, err := positive(value, "--width", defaultColumns); err == nil {
			t.Errorf("%q was accepted", value)
		}
	}
	if got, err := positive("", "--width", defaultColumns); err != nil || got != defaultColumns {
		t.Errorf("an absent width gave %d, %v", got, err)
	}
}

func TestAJournalKeepsOnlyItsEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal := `{"kind":"head","version":24,"meta":{}}
{"kind":"event","event":{"kind":"user_message","text":"hello"}}
{"kind":"items","items":[]}

{"event":{"kind":"model_message","text":"hi"}}
`
	if err := os.WriteFile(path, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	events, err := ReadJournal(path)
	if err != nil {
		t.Fatal(err)
	}

	kinds := []agent.Kind{}
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	if want := []agent.Kind{agent.UserMessageEvent, agent.ModelMessageEvent}; !slices.Equal(kinds, want) {
		t.Errorf("got %v, want %v", kinds, want)
	}
}

func TestAJournalWithoutEventsIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"head"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadJournal(path); err == nil {
		t.Error("an empty journal was accepted")
	}
}

func TestAnEscapedStreamIsSettledLikeARawOne(t *testing.T) {
	directory := t.TempDir()
	raw := filepath.Join(directory, "raw.ansi")
	escaped := filepath.Join(directory, "escaped.ansi")
	if err := os.WriteFile(raw, []byte("\x1b[1mbold\x1b[0m\r\nplain\x1b[1;1Hx"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(escaped, []byte(`\e[1mbold\e[0m\r`+"\n"+`plain\e[1;1Hx`), 0o600); err != nil {
		t.Fatal(err)
	}

	fromRaw, err := Settle(raw, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	fromEscaped, err := Settle(escaped, "", 20)
	if err != nil {
		t.Fatal(err)
	}

	if fromRaw != fromEscaped {
		t.Errorf("raw settled to %q, escaped to %q", fromRaw, fromEscaped)
	}
	if !strings.HasPrefix(fromRaw, "x\x1b[1mold") {
		t.Errorf("the cursor move was not followed: %q", fromRaw)
	}
}
