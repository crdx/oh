package ctl

import (
	"fmt"
	"os"

	"crdx.org/oh/internal/app/ctl/analyse"
	"crdx.org/oh/internal/app/ctl/check"
	"crdx.org/oh/internal/app/ctl/complete"
	"crdx.org/oh/internal/app/ctl/gc"
	"crdx.org/oh/internal/app/ctl/migrate"
	"crdx.org/oh/internal/app/ctl/models"
	"crdx.org/oh/internal/app/ctl/regenerate"
	"crdx.org/oh/internal/app/ctl/render"
	"crdx.org/oh/internal/app/ctl/sessions"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/style"
)

const Flag = "--ctl"

const usage = `oh --ctl — oh control

Usage:
    oh --ctl sessions [-j] [-w <dir>] [-r] [-a] [<filter>]
    oh --ctl sessions (-d | --delete) <label>
    oh --ctl check [options] [<session>...]
    oh --ctl analyse [options] [<session>...]
    oh --ctl regenerate [<session>...]
    oh --ctl migrate [options] [<session>...]
    oh --ctl gc [options]
    oh --ctl models [-I]
    oh --ctl render [options] <file>

Commands:
    sessions      List or delete stored sessions
    check         Check configuration and stored session health
    analyse       Analyse stored sessions
    regenerate    Write stored transcripts again from their journals
    migrate       Bring configuration and stored data up to date
    gc            Remove the caches sessions leave behind
    models        Print the qualified names of usable models
    render        Draw a journal, scene, or stream
`

func isCommand(argument string) bool {
	switch argument {
	case "sessions", "check", "analyse", "regenerate", "migrate", "gc", "models", "render":
		return true
	default:
		return false
	}
}

func Run(arguments []string, drawJournal render.DrawJournal) int {
	originalArguments := os.Args
	os.Args = append([]string{"oh", Flag}, arguments...)
	defer func() { os.Args = originalArguments }()

	if complete.Write(os.Stdout, arguments) {
		return 0
	}

	style.Init(os.Stdout)

	if len(arguments) < 1 {
		fmt.Print(usage)
		return 2
	}
	if !isCommand(arguments[0]) {
		fmt.Print(usage)
		return 2
	}
	if arguments[0] != "migrate" && arguments[0] != "render" {
		if err := location.RequireCurrentDataLayout(); err != nil {
			fmt.Fprintln(os.Stderr, style.Error(err))
			return 1
		}
	}

	var err error
	switch arguments[0] {
	case "sessions":
		err = sessions.Run()
	case "check":
		err = check.Run()
	case "analyse":
		err = analyse.Run()
	case "regenerate":
		err = regenerate.Run()
	case "migrate":
		err = migrate.Run()
	case "gc":
		err = gc.Run()
	case "models":
		err = models.Run()
	case "render":
		err = render.Run(drawJournal)
	default:
		fmt.Print(usage)
		return 2
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, style.Error(err))
		return 1
	}

	return 0
}
