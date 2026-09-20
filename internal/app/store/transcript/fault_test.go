package transcript

import (
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
)

func TestAppendFailureIsReturned(t *testing.T) {
	recorder, err := Open(t.TempDir()+"/chat.md", Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.file.Close(); err != nil {
		t.Fatal(err)
	}

	err = recorder.Event(time.Now(), agent.Event{Kind: agent.ModelMessageEvent, Text: "answer"})
	if err == nil {
		t.Fatal("expected append failure")
	}
}
