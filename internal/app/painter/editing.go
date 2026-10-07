package painter

import (
	"strings"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const (
	draftEditingLead = "Editing the draft in "
	draftEditingHint = "esc to keep the draft as it was"
)

func RenderDraftEditing(editorName string, columns int) []string {
	line := style.Change(draftEditingLead) + style.Subject(editorName)
	if columns <= 0 {
		return []string{line}
	}

	gap := columns - style.Width(line) - width.Of(draftEditingHint)
	if gap < 1 {
		return []string{width.Elide(line, columns)}
	}

	return []string{line + strings.Repeat(" ", gap) + style.Subtle(draftEditingHint)}
}
