package sessions

import (
	"fmt"

	"crdx.org/oh/internal/format"
	"crdx.org/oh/pkg/session"
)

func ValidateFormats(directory string) error {
	entries, err := session.Entries(directory)
	if err != nil {
		return err
	}

	return validateEntries(entries)
}

func validateEntries(entries []session.Entry) error {
	var ahead, outdatedNames []string
	for _, entry := range entries {
		if entry.Format > session.JournalFormat {
			ahead = append(ahead, entry.Name)
		} else if entry.Format < session.JournalFormat {
			outdatedNames = append(outdatedNames, entry.Name)
		}
	}

	if len(ahead) > 0 {
		return aheadError(ahead)
	}

	if len(outdatedNames) == 0 {
		return nil
	}

	return outdatedError(outdatedNames)
}

func explainFormat(name string, err error) error {
	switch {
	case format.IsNewer(err):
		return aheadError([]string{name})
	case format.IsOlder(err):
		return outdatedError([]string{name})
	default:
		return err
	}
}

func aheadError(names []string) error {
	subject, _ := nameSessions(names)

	return fmt.Errorf(
		"%s written in a newer journal format than this oh reads (format %d): upgrade oh",
		subject, session.JournalFormat,
	)
}

func outdatedError(names []string) error {
	subject, object := nameSessions(names)

	return fmt.Errorf(
		"%s written in an older journal format: run `oh --ctl migrate` to bring %s up to format %d",
		subject, object, session.JournalFormat,
	)
}

func nameSessions(names []string) (string, string) {
	if len(names) == 1 {
		return names[0] + " is", "it"
	}

	return fmt.Sprintf("%d sessions are", len(names)), "them"
}
