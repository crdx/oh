package ctl

import (
	"fmt"
	"os"

	"crdx.org/io/internal/app/ctl/analyse"
	"crdx.org/io/internal/app/ctl/complete"
	"crdx.org/io/internal/app/ctl/gc"
	"crdx.org/io/internal/app/ctl/migrate"
	"crdx.org/io/internal/app/ctl/regenerate"
	"crdx.org/io/internal/app/ctl/sessions"
	"crdx.org/io/internal/app/style"
)

const Flag = "--ctl"

const usage = `oh --ctl — oh control

Usage:
    oh --ctl sessions [options] [<filter>]
    oh --ctl analyse [options] [<session>...]
    oh --ctl regenerate [<session>...]
    oh --ctl migrate [options] [<session>...]
    oh --ctl gc [options]

Commands:
    sessions      List the stored sessions
    analyse       Analyse stored sessions
    regenerate    Write stored transcripts again from their journals
    migrate       Bring configuration and stored sessions up to their current formats
    gc            Remove the caches sessions leave behind
`

func Run(arguments []string) int {
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

	var err error
	switch arguments[0] {
	case "sessions":
		err = sessions.Run()
	case "analyse":
		err = analyse.Run()
	case "regenerate":
		err = regenerate.Run()
	case "migrate":
		err = migrate.Run()
	case "gc":
		err = gc.Run()
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
