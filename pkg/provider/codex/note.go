package codex

import "crdx.org/oh/pkg/agent"

var Notes agent.NoteFormat = notes{}

type notes struct{}

func (notes) Wrap(note agent.Note) string {
	switch note.Kind {
	case agent.InterruptionNote:
		return agent.WrapNote("turn_aborted", note.Text)
	case agent.HostCommandNote:
		return agent.WrapNote("user_shell_command", note.Text)
	case agent.EnvironmentNote:
		return agent.WrapNote("environment_context", note.Text)
	case agent.JobNote, agent.TitleNote, agent.PokeNote:
		return agent.WrapNote(`codex_internal_context source="`+string(note.Kind)+`"`, note.Text)
	}

	return agent.WrapNote(`codex_internal_context source="oh"`, note.Text)
}

func (notes) Rule() string {
	return "User messages may include <turn_aborted>, <user_shell_command>, <environment_context>, and <codex_internal_context> tags. They are added automatically by the harness, and are not written by the user."
}

func (self *Client) NoteFormat() agent.NoteFormat {
	return Notes
}
