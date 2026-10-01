package commands

import (
	"errors"
	"testing"

	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/work"
)

func TestTheShellCommandStartsWhatFollowsItVerbatim(t *testing.T) {
	workspaceDirectory := t.TempDir()
	var startedIn string
	var startedCommand string
	commands := newCommandRegistry(t, commandEnvironment{
		workspace: work.At(workspaceDirectory),
		startHostCommand: func(directory string, command string) error {
			startedIn = directory
			startedCommand = command

			return nil
		},
	})

	for _, input := range []string{"/!ls -la | wc -l", "/! ls -la | wc -l"} {
		t.Run(input, func(t *testing.T) {
			startedIn, startedCommand = "", ""
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected the shell command to be found")
			}

			context := &commandTestContext{}
			if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
				t.Fatal(err)
			}
			if startedCommand != "ls -la | wc -l" {
				t.Errorf("got command %q", startedCommand)
			}
			if startedIn != workspaceDirectory {
				t.Errorf("got directory %q, want %q", startedIn, workspaceDirectory)
			}
			if len(context.events) != 0 {
				t.Errorf("got events %+v, want the command's end to be reported when it comes", context.events)
			}
		})
	}
}

func TestTheShellCommandNeedsSomethingToRun(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		startHostCommand: func(string, string) error {
			t.Error("the command ran with nothing to run")

			return nil
		},
	})

	invocation, found := commands.Find("/!   ")
	if !found {
		t.Fatal("expected the shell command to be found")
	}
	err := invocation.Command.Run(&commandTestContext{}, invocation.Arguments)
	if !slash.IsUsageError(err) {
		t.Fatalf("got error %v", err)
	}
	if message := slash.FormatError(invocation, err); message != "Usage: /! <command>" {
		t.Errorf("got formatted error %q", message)
	}
}

func TestAShellThatCannotStartIsReported(t *testing.T) {
	failure := errors.New("a command is already running")
	commands := newCommandRegistry(t, commandEnvironment{
		startHostCommand: func(string, string) error { return failure },
	})

	invocation, found := commands.Find("/!ls")
	if !found {
		t.Fatal("expected the shell command to be found")
	}
	if err := invocation.Command.Run(&commandTestContext{}, invocation.Arguments); !errors.Is(err, failure) {
		t.Fatalf("got error %v", err)
	}
}

func TestTheShellCommandIsRefusedWhereNothingCanRunIt(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{workspace: work.At(t.TempDir())})

	invocation, found := commands.Find("/!ls")
	if !found {
		t.Fatal("expected the shell command to be found")
	}
	if err := invocation.Command.Run(&commandTestContext{}, invocation.Arguments); !errors.Is(err, errHostCommandsUnavailable) {
		t.Fatalf("got error %v", err)
	}
}
