package painter

import (
	"fmt"
	"strings"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/internal/app/width"
)

type DraftPurpose int

const (
	EditingTheDraft DraftPurpose = iota
	WritingAReply
)

func (self DraftPurpose) FailureMessage(reason string) string {
	if self == WritingAReply {
		return fmt.Sprintf(tr.ReplyDropped, reason)
	}
	return fmt.Sprintf(tr.DraftKept, reason)
}

func (self DraftPurpose) lead() string {
	if self == WritingAReply {
		return tr.WritingReplyLead
	}
	return tr.EditingDraftLead
}

func (self DraftPurpose) hint() string {
	if self == WritingAReply {
		return fmt.Sprintf(tr.DropReplyHint, "esc")
	}
	return fmt.Sprintf(tr.KeepDraftHint, "esc")
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
