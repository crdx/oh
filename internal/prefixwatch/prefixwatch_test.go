package prefixwatch

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func turn(role string, text string) json.RawMessage {
	return json.RawMessage(`{"role":"` + role + `","content":[{"type":"text","text":"` + text + `"}]}`)
}

func marked(role string, text string) json.RawMessage {
	return json.RawMessage(
		`{"role":"` + role + `","content":[{"cache_control":{"type":"ephemeral"},"type":"text","text":"` + text + `"}]}`,
	)
}

func TestTheFirstRequestOfAConversationHasNothingToCompare(t *testing.T) {
	var watcher Watcher
	if what := watcher.Look([]string{"read"}, "be brief", []json.RawMessage{turn("user", "go on")}); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestAConversationThatOnlyGrowsIsLeftAlone(t *testing.T) {
	var watcher Watcher
	held := []json.RawMessage{turn("user", "go on"), turn("assistant", "done")}
	watcher.Look([]string{"read"}, "be brief", held)

	grown := append(append([]json.RawMessage{}, held...), turn("user", "again"), turn("assistant", "done again"))
	if what := watcher.Look([]string{"read"}, "be brief", grown); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestABreakpointThatMovesIsNotARewrite(t *testing.T) {
	var watcher Watcher
	watcher.Look([]string{"read"}, "be brief", []json.RawMessage{marked("user", "go on"), turn("assistant", "done")})

	moved := []json.RawMessage{turn("user", "go on"), marked("assistant", "done"), turn("user", "again")}
	if what := watcher.Look([]string{"read"}, "be brief", moved); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestATailTheModelHasNotAnsweredMayStillGrow(t *testing.T) {
	var watcher Watcher
	watcher.Look([]string{"read"}, "be brief", []json.RawMessage{turn("user", "go on")})

	if what := watcher.Look([]string{"read"}, "be brief", []json.RawMessage{turn("user", "go on and again")}); what != "" {
		t.Errorf("got %q", what)
	}
}

func TestEveryWayOfRewritingWhatWasSentIsNamed(t *testing.T) {
	held := []json.RawMessage{turn("user", "go on"), turn("assistant", "done"), turn("user", "again")}

	for name, test := range map[string]struct {
		tools  []string
		system string
		turns  []json.RawMessage
		want   string
	}{
		"a tool appears": {
			tools: []string{"read", "job"}, system: "be brief", turns: held, want: ToolsChanged,
		},
		"the system prompt gains a block": {
			tools: []string{"read"}, system: "be brief, and careful", turns: held, want: SystemChanged,
		},
		"an answered turn is rewritten": {
			tools: []string{"read"}, system: "be brief",
			turns: []json.RawMessage{turn("user", "go on differently"), turn("assistant", "done"), turn("user", "again")},
			want:  TurnChanged + " (1 of 2)",
		},
		"an answered turn is fused into another": {
			tools: []string{"read"}, system: "be brief",
			turns: []json.RawMessage{turn("user", "go on"), turn("assistant", "done and more")},
			want:  TurnChanged + " (2 of 2)",
		},
		"the conversation is cut short": {
			tools: []string{"read"}, system: "be brief",
			turns: []json.RawMessage{turn("user", "go on")},
			want:  TurnsDropped,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var watcher Watcher
			watcher.Look([]string{"read"}, "be brief", held)

			if what := watcher.Look(test.tools, test.system, test.turns); what != test.want {
				t.Errorf("got %q, want %q", what, test.want)
			}
		})
	}
}

func TestATurnRewrittenAfterItsMarkWasReusedIsStillNamed(t *testing.T) {
	var watcher Watcher
	held := []json.RawMessage{turn("user", "go on"), turn("assistant", "done")}
	watcher.Look([]string{"read"}, "be brief", held)
	grown := append(append([]json.RawMessage{}, held...), turn("user", "again"), turn("assistant", "done again"))
	watcher.Look([]string{"read"}, "be brief", grown)

	rewritten := append([]json.RawMessage{turn("user", "go on differently")}, grown[1:]...)
	if what := watcher.Look([]string{"read"}, "be brief", rewritten); what != TurnChanged+" (1 of 4)" {
		t.Errorf("got %q", what)
	}
}

func FuzzReusedMarksGiveTheVerdictFreshMarksWould(f *testing.F) {
	f.Add("go on", "done", "go on", "done", "again", true)
	f.Add("go on", "done", "go on differently", "done", "", false)
	f.Add(`{"role":"user"}`, "done", "go on", `x"`, "again", true)

	f.Fuzz(func(t *testing.T, first string, answer string, firstAgain string, answerAgain string, next string, isMarked bool) {
		held := []json.RawMessage{turn("user", first), turn("assistant", answer)}
		later := []json.RawMessage{turn("user", firstAgain), turn("assistant", answerAgain), turn("user", next)}
		if isMarked {
			later[1] = marked("assistant", answerAgain)
		}

		var reusing, fresh Watcher
		reusing.Look([]string{"read"}, "be brief", held)
		fresh.Look([]string{"read"}, "be brief", held)
		for at := range fresh.turns {
			fresh.turns[at].raw = mark{}
		}

		if reused, recomputed := reusing.Look([]string{"read"}, "be brief", later), fresh.Look([]string{"read"}, "be brief", later); reused != recomputed {
			t.Errorf("reusing marks said %q where fresh marks said %q", reused, recomputed)
		}
		if !slices.Equal(reusing.turns, fresh.turns) || reusing.bound != fresh.bound {
			t.Errorf("reusing marks remembered %v (bound %d), fresh marks %v (bound %d)", reusing.turns, reusing.bound, fresh.turns, fresh.bound)
		}
	})
}

func BenchmarkLookAtAGrowingConversation(b *testing.B) {
	turns := make([]json.RawMessage, 0, 400)
	for at := range 400 {
		role := []string{"user", "assistant"}[at%2]
		turns = append(turns, turn(role, strings.Repeat("the quick brown fox jumps over the lazy dog ", 40)))
	}

	var watcher Watcher
	watcher.Look([]string{"read"}, "be brief", turns[:len(turns)-1])

	b.ReportAllocs()
	for b.Loop() {
		watcher.Look([]string{"read"}, "be brief", turns)
	}
}
