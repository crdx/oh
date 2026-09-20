package title_test

import (
	"encoding/json"
	"strings"
	"testing"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/title"
)

func call(t *testing.T, titleTool tool.Tool, arguments string) tool.ToolCallResult {
	t.Helper()

	parsedCall, err := titleTool.Parse(arguments)
	if err != nil {
		t.Fatalf("could not parse %s: %v", arguments, err)
	}

	result, err := parsedCall.Exec(t.Context())
	if err != nil {
		t.Fatalf("could not run %s: %v", arguments, err)
	}

	return result
}

func titleOf(t *testing.T, state json.RawMessage) string {
	t.Helper()

	var decoded agent.TitleState
	if err := json.Unmarshal(state, &decoded); err != nil {
		t.Fatalf("could not read the state %s: %v", state, err)
	}

	return decoded.Title
}

func TestTitlingASessionRecordsDurableState(t *testing.T) {
	titleTool := title.New()

	if key := titleTool.StateKey(); key != agent.TitleStateKey {
		t.Errorf("the tool owns %q, want %q", key, agent.TitleStateKey)
	}
	if !titleTool.ReadOnly() || !titleTool.Concurrent() {
		t.Error("expected titling to change nothing and to run beside other calls")
	}

	result := call(t, titleTool, `{"title": "fix the picker clipping"}`)
	if got := titleOf(t, result.State); got != "fix the picker clipping" {
		t.Errorf("recorded %q", got)
	}
	if !strings.Contains(result.Output, `session titled "fix the picker clipping"`) {
		t.Errorf("said %q", result.Output)
	}
}

func TestTitlingFlattensWhatItIsGiven(t *testing.T) {
	titleTool := title.New()

	result := call(t, titleTool, `{"title": "  fix\tthe\npicker \u001b[31mclipping  "}`)
	if got := titleOf(t, result.State); got != "fix the picker clipping" {
		t.Errorf("recorded %q", got)
	}

	parsedCall, err := titleTool.Parse(`{"title": "  fix\tthe\npicker clipping  "}`)
	if err != nil {
		t.Fatalf("could not parse: %v", err)
	}
	if subject := parsedCall.Subject(); subject != "fix the picker clipping" {
		t.Errorf("drew %q", subject)
	}
}

func TestTitlingASessionWhatItIsCalledChangesNothing(t *testing.T) {
	titleTool := title.New()

	first := call(t, titleTool, `{"title": "fix the picker clipping"}`)
	if err := titleTool.Restore(first.State); err != nil {
		t.Fatalf("could not apply the title: %v", err)
	}

	again := call(t, titleTool, `{"title": "fix the picker clipping"}`)
	if len(again.State) != 0 {
		t.Errorf("titling it the same thing recorded %s", again.State)
	}
	if !strings.Contains(again.Output, "title unchanged") {
		t.Errorf("said %q", again.Output)
	}

	changed := call(t, titleTool, `{"title": "give sessions a title"}`)
	if got := titleOf(t, changed.State); got != "give sessions a title" {
		t.Errorf("recorded %q", got)
	}
}

func TestARestoredSessionRemembersItsTitle(t *testing.T) {
	stored, err := json.Marshal(agent.TitleState{Title: "fix the picker clipping"})
	if err != nil {
		t.Fatal(err)
	}

	titleTool := title.New()
	if err := titleTool.Restore(stored); err != nil {
		t.Fatalf("could not restore: %v", err)
	}

	if result := call(t, titleTool, `{"title": "fix the picker clipping"}`); len(result.State) != 0 {
		t.Errorf("a restored session recorded the title it already had: %s", result.State)
	}

	if err := titleTool.Restore(json.RawMessage(`{`)); err == nil {
		t.Error("expected malformed state to be refused")
	}
}

func TestAnUnusableTitleIsRefused(t *testing.T) {
	const longestTitleLength = 50

	titleTool := title.New()

	for _, arguments := range []string{
		`{"title": ""}`,
		`{"title": "   \n  "}`,
		`{"title": "` + strings.Repeat("a", longestTitleLength+1) + `"}`,
	} {
		if _, err := titleTool.Parse(arguments); err == nil {
			t.Errorf("expected %s to be refused", arguments)
		}
	}

	if _, err := titleTool.Parse(`{"title": "` + strings.Repeat("a", longestTitleLength) + `"}`); err != nil {
		t.Errorf("expected the longest usable title to be taken: %v", err)
	}
}
