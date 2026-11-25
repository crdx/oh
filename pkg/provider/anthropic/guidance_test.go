package anthropic_test

import (
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/anthropic"
)

var namesNothingOhOffers = []string{
	"CLAUDE.md",
	"TodoWrite",
	"ToolSearch",
	"AskUserQuestion",
	"${",
	"scratchpad",
	"compact",
	"Output Style",
	"without a prior",
}

func TestTheGuidanceOpensWithASection(t *testing.T) {
	if guidance := anthropic.Guidance(); !strings.HasPrefix(guidance, "## ") {
		t.Errorf("the guidance does not open with a section heading: %q", guidance[:min(len(guidance), 40)])
	}
}

func TestTheGuidanceNamesNothingOhLacks(t *testing.T) {
	guidance := anthropic.Guidance()
	for _, name := range namesNothingOhOffers {
		if strings.Contains(guidance, name) {
			t.Errorf("the guidance names %q", name)
		}
	}
}

func TestEveryClaudeModelGetsTheGuidance(t *testing.T) {
	for _, model := range []string{"claude-opus-5", "claude-haiku-5-5", "claude-unreleased-9"} {
		client, err := anthropic.New(anthropic.Static("access"), model, "high", 1_000)
		if err != nil {
			t.Fatal(err)
		}

		if got := agent.GuidanceOf(client); got != anthropic.Guidance() {
			t.Errorf("%s got %q", model, got)
		}
	}
}
