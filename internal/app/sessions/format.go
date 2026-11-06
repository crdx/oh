package sessions

import (
	"fmt"

	"crdx.org/oh/internal/app/tr"
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

const migrateCommand = "oh --ctl migrate"

func aheadError(names []string) error {
	if len(names) == 1 {
		return fmt.Errorf(tr.NewerFormatSession, names[0], session.JournalFormat)
	}

	return fmt.Errorf(tr.NewerFormatSessions, len(names), session.JournalFormat)
}

func outdatedError(names []string) error {
	if len(names) == 1 {
		return fmt.Errorf(tr.OlderFormatSession, names[0], migrateCommand, session.JournalFormat)
	}

	return fmt.Errorf(tr.OlderFormatSessions, len(names), migrateCommand, session.JournalFormat)
}
