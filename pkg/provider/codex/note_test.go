package codex_test

import (
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/codex"
)

func TestEachKindOfNoteTakesTheTagCodexGivesIt(t *testing.T) {
	client, err := codex.New(codex.Static("access", "refresh"), "gpt-5.5", "high")
	if err != nil {
		t.Fatal(err)
	}
	format := agent.NoteFormatOf(client)

	for _, scenario := range []struct {
		kind agent.NoteKind
		want string
	}{
		{agent.InterruptionNote, "<turn_aborted>\nnote\n</turn_aborted>"},
		{agent.HostCommandNote, "<user_shell_command>\nnote\n</user_shell_command>"},
		{agent.EnvironmentNote, "<environment_context>\nnote\n</environment_context>"},
		{agent.JobNote, "<codex_internal_context source=\"job\">\nnote\n</codex_internal_context>"},
		{agent.TitleNote, "<codex_internal_context source=\"title\">\nnote\n</codex_internal_context>"},
		{agent.PokeNote, "<codex_internal_context source=\"poke\">\nnote\n</codex_internal_context>"},
	} {
		if got := format.Wrap(agent.Note{Kind: scenario.kind, Text: "note"}); got != scenario.want {
			t.Errorf("%s: got %q, want %q", scenario.kind, got, scenario.want)
		}
	}
}
