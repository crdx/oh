package painter

import (
	"strings"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

type DraftPurpose int

const (
	EditingTheDraft DraftPurpose = iota
	WritingAReply
)

func (self DraftPurpose) FailureLead() string {
	if self == WritingAReply {
		return "The reply was dropped: "
	}
	return "The draft was kept as it was: "
}

func (self DraftPurpose) lead() string {
	if self == WritingAReply {
		return "Writing the reply in "
	}
	return "Editing the draft in "
}

func (self DraftPurpose) hint() string {
	if self == WritingAReply {
		return "esc to drop the reply"
	}
	return "esc to keep the draft as it was"
}

func RenderDraftEditing(editorName string, purpose DraftPurpose, columns int) []string {
	line := style.Change(purpose.lead()) + style.Subject(editorName)
	hint := purpose.hint()
	if columns <= 0 {
		return []string{line}
	}

	gap := columns - style.Width(line) - width.Of(hint)
	if gap < 1 {
		return []string{width.Elide(line, columns)}
	}

	return []string{line + strings.Repeat(" ", gap) + style.Subtle(hint)}
}
