package commands

import (
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/tr"
)

const (
	shellCommandName  = "!"
	shellCommandUsage = "<command>"
)

func shellCommand(environment commandEnvironment) slash.Command {
	return slash.Command{
		Name:        shellCommandName,
		Description: tr.HostCommandDescription,
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if arguments.Text == "" {
				return slash.Usage()
			}

			return environment.startHostCommand(environment.workspace.GetDir(), arguments.Text)
		},
	}.WithAttachedArgument(shellCommandUsage)
}
