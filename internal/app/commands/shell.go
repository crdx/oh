package commands

import (
	"crdx.org/oh/internal/app/slash"
)

const (
	shellCommandName  = "!"
	shellCommandUsage = "<command>"
)

func shellCommand(environment commandEnvironment) slash.Command {
	return slash.Command{
		Name:        shellCommandName,
		Description: "run a command on the host and tell the model what it printed",
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if arguments.Text == "" {
				return slash.Usage()
			}

			return environment.startHostCommand(environment.workspace.GetDir(), arguments.Text)
		},
	}.WithAttachedArgument(shellCommandUsage)
}
