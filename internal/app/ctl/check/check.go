package check

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

	"crdx.org/duckopt/v2"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/session"
)

const usage = `oh --ctl check — check configuration and stored session health

Usage:
    $0 --ctl check [<session>...]

Options:
    -h, --help    Show this help

The global configuration and the oh.toml in the current directory are checked. Sessions are named
on the command line, or every stored and archived session is checked when none is named. A session
check reads its complete journal and listing metadata, and verifies the contents of an archive.
`

type inputOpts struct {
	IsControlling bool     `docopt:"--ctl"`
	Check         bool     `docopt:"check"`
	Sessions      []string `docopt:"<session>"`
}

type paths struct {
	configSources []config.Source
	sessionsDir   string
}

type candidate struct {
	name   string
	target string
	err    error
}

func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}

	return run(paths{
		configSources: []config.Source{
			{Path: location.GetConfigFile()},
			{Path: filepath.Join(workingDirectory, "oh.toml"), IsOverride: true},
		},
		sessionsDir: location.GetSessionsDir(),
	}, options.Sessions, console.Standard())
}

func run(checkPaths paths, requestedNames []string, output console.Output) error {
	failureCount := 0
	if _, err := config.LoadSources(checkPaths.configSources...); err != nil {
		failureCount++
		writeFailure(output.Failure, "configuration", err)
	} else {
		_, _ = fmt.Fprintln(output.Screen, style.Success("ok")+" configuration")
	}

	candidates, err := sessionCandidates(checkPaths.sessionsDir, requestedNames)
	if err != nil {
		failureCount++
		writeFailure(output.Failure, "session store", err)
	} else {
		sessionFailureCount := 0
		for index, err := range checkSessions(checkPaths.sessionsDir, candidates) {
			if err != nil {
				failureCount++
				sessionFailureCount++
				writeFailure(output.Failure, candidates[index].target, err)
			}
		}

		sessionCount := len(candidates)
		switch {
		case sessionCount == 0:
			_, _ = fmt.Fprintln(output.Screen, style.Success("ok")+" session store (no sessions)")
		case sessionFailureCount == 0:
			_, _ = fmt.Fprintln(output.Screen, style.Success("ok")+" "+util.Plural(sessionCount, "session"))
		default:
			_, _ = fmt.Fprintln(output.Screen, style.Subtle("checked "+util.Plural(sessionCount, "session")))
		}
	}

	if failureCount > 0 {
		return fmt.Errorf("%s failed", util.Plural(failureCount, "health check"))
	}

	return nil
}

func sessionCandidates(directory string, requestedNames []string) ([]candidate, error) {
	if len(requestedNames) > 0 {
		candidates := make([]candidate, 0, len(requestedNames))
		for _, name := range requestedNames {
			candidates = append(candidates, candidate{name: name, target: name})
		}
		return candidates, nil
	}

	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		entryName := entry.Name()
		switch {
		case entry.IsDir() && session.IsName(entryName):
			candidates = append(candidates, candidate{name: entryName, target: entryName})
		case entry.Type().IsRegular() && strings.HasSuffix(entryName, session.ArchiveSuffix):
			name := strings.TrimSuffix(entryName, session.ArchiveSuffix)
			if session.IsName(name) {
				candidates = append(candidates, candidate{name: name, target: entryName})
				continue
			}
			candidates = append(candidates, candidate{target: entryName, err: errors.New("unrecognised entry in the session store")})
		default:
			candidates = append(candidates, candidate{target: entryName, err: errors.New("unrecognised entry in the session store")})
		}
	}

	slices.SortFunc(candidates, func(first candidate, second candidate) int {
		return strings.Compare(first.target, second.target)
	})
	return candidates, nil
}

func checkSessions(directory string, candidates []candidate) []error {
	failures := make([]error, len(candidates))
	jobs := make(chan int, len(candidates))
	for index := range candidates {
		jobs <- index
	}
	close(jobs)

	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(candidates)) {
		workers.Go(func() {
			for index := range jobs {
				failures[index] = checkSession(directory, candidates[index])
			}
		})
	}
	workers.Wait()
	return failures
}

func checkSession(directory string, one candidate) error {
	if one.err != nil {
		return one.err
	}
	if err := session.Check(directory, one.name); err != nil {
		return err
	}

	isInUse, err := session.IsInUse(directory, one.name)
	if err != nil {
		return err
	}
	if isInUse {
		return nil
	}

	actual, err := session.ReadMeta(directory, one.name)
	if err != nil {
		return err
	}
	expectedMeta, err := store.GetListingMeta(directory, one.name)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expectedMeta) {
		return errors.New("listing metadata does not match the journal")
	}

	return nil
}

func writeFailure(writer io.Writer, target string, err error) {
	_, _ = fmt.Fprintln(writer, style.Failure("failed")+" "+target+": "+err.Error())
}
