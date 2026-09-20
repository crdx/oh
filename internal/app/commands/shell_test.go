package commands

import (
	"errors"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/work"
)

func TestTheShellCommandRunsWhatFollowsItVerbatim(t *testing.T) {
	workspaceDirectory := t.TempDir()
	var ranIn string
	var ranCommand string
	commands := newCommandRegistry(t, commandEnvironment{
		workspace: work.At(workspaceDirectory),
		runHostCommand: func(directory string, command string) (hostcommand.Result, error) {
			ranIn = directory
			ranCommand = command

			return hostcommand.Result{Command: command, Output: "readme\n"}, nil
		},
	})

	for _, input := range []string{"/!ls -la | wc -l", "/! ls -la | wc -l"} {
		t.Run(input, func(t *testing.T) {
			ranIn, ranCommand = "", ""
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected the shell command to be found")
			}

			context := &commandTestContext{}
			if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
				t.Fatal(err)
			}
			if ranCommand != "ls -la | wc -l" {
				t.Errorf("got command %q", ranCommand)
			}
			if ranIn != workspaceDirectory {
				t.Errorf("got directory %q, want %q", ranIn, workspaceDirectory)
			}
			if len(context.events) != 1 || context.events[0].Kind != hostcommand.Ran {
				t.Fatalf("got events %+v", context.events)
			}
			notice, isSaid := hostcommand.Notice(context.events[0])
			if !isSaid || !strings.Contains(notice, "ls -la | wc -l") {
				t.Errorf("got notice %q", notice)
			}
		})
	}
}

func TestTheShellCommandCapsWhatItTellsTheModel(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		runHostCommand: func(string, string) (hostcommand.Result, error) {
			return hostcommand.Result{Command: "ls", Output: "a very long listing"}, nil
		},
		limitOutput: func(string) string { return "a very…" },
	})

	invocation, found := commands.Find("/!ls")
	if !found {
		t.Fatal("expected the shell command to be found")
	}
	context := &commandTestContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}

	notice, isSaid := hostcommand.Notice(context.events[0])
	if !isSaid || !strings.Contains(notice, "\na very…\n") {
		t.Errorf("got notice %q", notice)
	}
}

func TestTheShellCommandNeedsSomethingToRun(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		runHostCommand: func(string, string) (hostcommand.Result, error) {
			t.Error("the command ran with nothing to run")

			return hostcommand.Result{}, nil
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

func TestAShellThatCannotStartIsReportedRatherThanSent(t *testing.T) {
	failure := errors.New("bash could not start")
	commands := newCommandRegistry(t, commandEnvironment{
		runHostCommand: func(string, string) (hostcommand.Result, error) {
			return hostcommand.Result{}, failure
		},
	})

	invocation, found := commands.Find("/!ls")
	if !found {
		t.Fatal("expected the shell command to be found")
	}
	context := &commandTestContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); !errors.Is(err, failure) {
		t.Fatalf("got error %v", err)
	}
	if len(context.events) != 0 {
		t.Errorf("got events %+v", context.events)
	}
}
