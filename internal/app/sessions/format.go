package sessions

import (
	"fmt"

	"crdx.org/io/pkg/session"
)

func ValidateFormats(directory string) error {
	entries, err := session.Entries(directory)
	if err != nil {
		return err
	}

	return validateEntries(entries)
}

func ValidateStoredFormats(directory string) error {
	entries, err := session.StoredEntries(directory)
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
		subject, _ := nameSessions(ahead)
		return fmt.Errorf(
			"%s written in a newer journal format than this oh reads (format %d): upgrade oh",
			subject, session.JournalFormat,
		)
	}

	if len(outdatedNames) == 0 {
		return nil
	}

	subject, object := nameSessions(outdatedNames)

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
