package codex_test

import (
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/codex"
)

var namesNothingOhOffers = []string{
	"apply_patch",
	"exec_command",
	"functions.",
	"multi_tool_use",
	"request_user_input",
	"send_user_message",
	"skills.list",
	"CODEX_HOME",
	"commentary",
	"final channel",
	"compact",
	"plugin",
	"auto-review",
}

func TestTheGuidanceOpensWithASection(t *testing.T) {
	if guidance := codex.Guidance(); !strings.HasPrefix(guidance, "## ") {
		t.Errorf("the guidance does not open with a section heading: %q", guidance[:min(len(guidance), 40)])
	}
}

func TestTheGuidanceNamesNothingOhLacks(t *testing.T) {
	guidance := codex.Guidance()
	for _, name := range namesNothingOhOffers {
		if strings.Contains(guidance, name) {
			t.Errorf("the guidance names %q", name)
		}
	}
}

func TestEveryCodexModelGetsTheGuidance(t *testing.T) {
	for _, model := range []string{"gpt-5.5", "gpt-6.2-sol", "gpt-9-unreleased"} {
		client, err := codex.New(codex.Static("access", "account"), model, "high")
		if err != nil {
			t.Fatal(err)
		}

		if got := agent.GuidanceOf(client); got != codex.Guidance() {
			t.Errorf("%s got %q", model, got)
		}
	}
}
