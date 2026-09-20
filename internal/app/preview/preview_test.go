package preview

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/app/portgrant"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func conversation() []agent.Event {
	return []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "why does the spinner stutter when a tool runs"},
		{Kind: agent.ModelReasoningEvent, Text: "Looking for where the spinner is drawn."},
		{Kind: agent.ModelMessageEvent, Text: "I'll have a look at how the spinner is drawn."},
		{
			Kind:      agent.ToolCallRequestEvent,
			ID:        "call-1",
			Name:      "grep",
			Arguments: `{"pattern":"spinner"}`,
			FallbackRendering: agent.FallbackRendering{
				Subject:  "spinner",
				Note:     "*.go",
				ReadOnly: true,
			},
		},
		{Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "grep", Status: agent.SuccessStatus, Text: "1 line"},
		{
			Kind: agent.ModelMessageEvent,
			Text: "The spinner is redrawn on every beat, which is why it stutters while a tool holds the line.",
		},
	}
}

func exposedPorts(t *testing.T) []agent.Event {
	t.Helper()

	var events []agent.Event
	var ports []uint16

	for _, port := range []uint16{8001, 8002, 8003} {
		ports = append(ports, port)
		event, err := portgrant.HostToSandboxChangeEvent("127.9.9.9", port, ports)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}

	return events
}

func contextExceeded() []agent.Event {
	return []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "carry on with the refactor"},
		{
			Kind: agent.FailureEvent,
			Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Code:       "exceed_context_size_error",
				Message:    "request (264481 tokens) exceeds the available context size (262144 tokens)",
			},
		},
	}
}

func TestGoldenWhatAConversationLooksLikeBeforeItIsOpenedMatchesTheGolden(t *testing.T) {
	var drawn strings.Builder

	for _, room := range []int{100, 46} {
		fmt.Fprintf(&drawn, "=== %d columns ===\n", room)
		for _, row := range Draw(conversation(), nil, room) {
			fmt.Fprintln(&drawn, row)
		}
		fmt.Fprintf(&drawn, "=== %d columns, ports exposed in a row ===\n", room)
		for _, row := range Draw(exposedPorts(t), nil, room) {
			fmt.Fprintln(&drawn, row)
		}
		fmt.Fprintf(&drawn, "=== %d columns, the context window filled ===\n", room)
		for _, row := range Draw(contextExceeded(), nil, room) {
			fmt.Fprintln(&drawn, row)
		}
	}

	compareWithGolden(t, "conversation.ansi", strutil.VisibleEscapes(drawn.String()))
}

func TestAStoredConversationIsReadFromItsJournal(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range conversation() {
		if err := log.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := Read(directory, log.Name(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}

	if want := Draw(conversation(), nil, 100); !slices.Equal(rows, want) {
		t.Errorf("the stored conversation was read as %q, want %q", rows, want)
	}
}

func TestAConversationThatWasNeverStoredIsReported(t *testing.T) {
	if _, err := Read(t.TempDir(), "tame-impala", nil, 100); err == nil {
		t.Error("expected a missing session to be reported")
	}
}

func TestANarrowTerminalIsGivenTheLeastRoomAConversationCanBeDrawnIn(t *testing.T) {
	narrow := Draw(conversation(), nil, 1)
	least := Draw(conversation(), nil, minimumRoom)

	if !slices.Equal(narrow, least) {
		t.Error("expected a terminal narrower than the minimum to be drawn at the minimum")
	}
}

func TestNothingStoredIsDrawnAsNoRows(t *testing.T) {
	if rows := Draw(nil, nil, 100); len(rows) != 0 {
		t.Errorf("expected nothing to be drawn, got %q", rows)
	}
}

func compareWithGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if drawn != string(want) {
		t.Errorf("the conversation differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
