package commands

import (
	"strings"

	"crdx.org/oh/internal/app/slash"
)

const quoteMark = ">"

func replyCommand(getLastMessage func() (string, bool), replyInEditor func(string) error) slash.Command {
	lastMessage := lastMessageTarget(getLastMessage)
	return slash.Command{
		Name:        "reply",
		Description: "reply inline to the last answer in your editor",
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			values, err := lastMessage.resolveValues()
			if err != nil {
				return err
			}
			return replyInEditor(QuoteReply(values[0]))
		},
	}
}

func QuoteReply(message string) string {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = quoteMark
			continue
		}
		lines[i] = quoteMark + " " + line
	}

	return strings.Join(lines, "\n") + "\n\n"
}
