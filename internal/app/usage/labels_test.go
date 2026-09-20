package usage

import (
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
)

func TestAWindowIsNamedForTheSubscriptionItMeters(t *testing.T) {
	for _, test := range []struct {
		name   string
		window agent.UsageWindow
		want   string
	}{
		{name: "the session window", window: agent.UsageWindow{Duration: 5 * time.Hour}, want: "Session"},
		{name: "the weekly window", window: agent.UsageWindow{Duration: 7 * 24 * time.Hour}, want: "Week"},
		{name: "the monthly window", window: agent.UsageWindow{Duration: 30 * 24 * time.Hour}, want: "Month"},
		{name: "a window of its own length", window: agent.UsageWindow{Duration: 90 * time.Minute}, want: "90m"},
		{
			name:   "a scoped window",
			window: agent.UsageWindow{Duration: 5 * time.Hour, Scope: "gpt-5.3-codex-spark"},
			want:   "Spark Session",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := WindowLabel(test.window); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestAScopeIsNamedAsAPersonWouldSayIt(t *testing.T) {
	for _, test := range []struct {
		scope string
		want  string
	}{
		{scope: "gpt-5.3-codex-spark", want: "Spark"},
		{scope: "claude-opus-4-6", want: "Opus 4.6"},
		{scope: "claude-fable-5", want: "Fable 5"},
		{scope: "claude-fable-5-1", want: "Fable 5.1"},
		{scope: "opus", want: "Opus"},
		{scope: "sonnet", want: "Sonnet"},
		{scope: "gpt-5", want: "GPT 5"},
		{scope: "-", want: "-"},
	} {
		t.Run(test.scope, func(t *testing.T) {
			if got := ScopeLabel(test.scope); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}
