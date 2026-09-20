package commands

import (
	"crdx.org/io/internal/app/hostcommand"
	"crdx.org/io/internal/app/slash"
)

const (
	shellCommandName  = "!"
	shellCommandUsage = "<command>"
)

func shellCommand(environment commandEnvironment) slash.Command {
	return slash.Command{
		Name:        shellCommandName,
		Description: "run a command on the host and tell the model what it printed",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text == "" {
				return slash.Usage()
			}

			result, err := environment.runHostCommand(environment.workspace.GetDir(), arguments.Text)
			if err != nil {
				return err
			}
			result.Output = environment.limitOutput(result.Output)
			context.Emit(hostcommand.RanEvent(result))

			return nil
		},
	}.WithAttachedArgument(shellCommandUsage)
}
