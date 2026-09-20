package regenerate

import (
	"fmt"

	"crdx.org/duckopt/v2"
	"crdx.org/io/internal/app/ctl/console"
	"crdx.org/io/internal/app/location"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/session"
)

const usage = `oh --ctl regenerate — write stored transcripts again

Usage:
    $0 --ctl regenerate [<session>...]

Sessions are named on the command line, or every stored session is done when the command names no session.
`

type inputOpts struct {
	IsControlling bool     `docopt:"--ctl"`
	Regenerate    bool     `docopt:"regenerate"`
	Sessions      []string `docopt:"<session>"`
}

func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")
	return run(location.GetSessionsDir(), options.Sessions, console.Standard())
}

func run(directory string, sessions []string, output console.Output) error {
	names := sessions
	if len(names) == 0 {
		var err error
		if names, err = storedNames(directory); err != nil {
			return err
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("there are no stored sessions in %s", directory)
	}

	failures := 0
	for _, name := range names {
		if err := store.Rebuild(directory, name); err != nil {
			failures++
			_, _ = fmt.Fprintln(output.Failure, style.Failure(name+": "+err.Error()))
			continue
		}
		_, _ = fmt.Fprintln(output.Screen, style.Subtle("wrote ")+name)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d could not be written", failures, len(names))
	}

	_, _ = fmt.Fprintln(output.Screen, style.Subtle(util.Plural(len(names), "transcript")+" written"))
	return nil
}

func storedNames(directory string) ([]string, error) {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names, nil
}
