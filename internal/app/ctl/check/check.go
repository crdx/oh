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
	"time"

	"crdx.org/duckopt/v2"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/format"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/session"
)

const Usage = `oh --ctl check — check configuration and stored session health

Usage:
    $0 --ctl check [options] [<session>...]

Options:
    -f, --fix        Repair deterministic session problems
    -n, --dry-run    Report repairs without writing them; requires --fix
    -h, --help       Show this help
`

const maxCheckWorkers = 4

type inputOpts struct {
	IsControlling bool     `docopt:"--ctl"`
	Check         bool     `docopt:"check"`
	Fix           bool     `docopt:"--fix"`
	DryRun        bool     `docopt:"--dry-run"`
	Sessions      []string `docopt:"<session>"`
}

type options struct {
	isFixing bool
	isDryRun bool
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

type healthState int

const (
	healthy healthState = iota
	running
	recoverable
	repairable
	invalid
)

type repair string

const (
	repairTail       repair = "remove an incomplete final journal record"
	repairTurn       repair = "abandon an unfinished turn"
	repairMeta       repair = "rebuild listing metadata"
	repairTranscript repair = "rebuild the transcript"
)

type result struct {
	candidate candidate
	state     healthState
	repairs   []repair
	err       error
	archived  bool
}

func Run() error {
	arguments := duckopt.MustBind[inputOpts](Usage, "$0")
	if arguments.DryRun && !arguments.Fix {
		return errors.New("--dry-run requires --fix")
	}
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
	}, arguments.Sessions, options{isFixing: arguments.Fix, isDryRun: arguments.DryRun}, console.Standard())
}

func run(checkPaths paths, requestedNames []string, settings options, output console.Output) error {
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
		results := checkSessions(checkPaths.sessionsDir, candidates)
		sessionFailureCount := 0
		fixedCount := 0
		for index := range results {
			didFail, wasFixed := handleResult(checkPaths.sessionsDir, settings, output, &results[index])
			if didFail {
				failureCount++
				sessionFailureCount++
			}
			if wasFixed {
				fixedCount++
			}
		}

		sessionCount := len(candidates)
		switch {
		case sessionCount == 0:
			_, _ = fmt.Fprintln(output.Screen, style.Success("ok")+" session store (no sessions)")
		case fixedCount > 0 && sessionFailureCount == 0:
			_, _ = fmt.Fprintln(output.Screen, style.Success("fixed")+" "+fmt.Sprintf("%d of %s", fixedCount, util.Plural(sessionCount, "session")))
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

func handleResult(directory string, settings options, output console.Output, sessionResult *result) (bool, bool) {
	switch sessionResult.state {
	case healthy:
		return false, false
	case running:
		_, _ = fmt.Fprintln(output.Screen, style.Subtle("skipped "+sessionResult.candidate.target+" in use"))
		return false, false
	case invalid:
		writeFailure(output.Failure, sessionResult.candidate.target, sessionResult.err)
		return true, false
	case recoverable:
		if !settings.isFixing {
			writeWarning(output.Screen, sessionResult.candidate.target, "unfinished turn is recoverable")
			return false, false
		}
	case repairable:
		if !settings.isFixing {
			writeRepairable(output.Failure, sessionResult)
			return true, false
		}
	}

	if sessionResult.archived {
		writeFailure(output.Failure, sessionResult.candidate.target, errors.New("restore the archived session before fixing it"))
		return true, false
	}
	if settings.isDryRun {
		writePlanned(output.Screen, sessionResult)
		return sessionResult.state == repairable, false
	}
	if err := fixSession(directory, *sessionResult); err != nil {
		writeFailure(output.Failure, sessionResult.candidate.target, err)
		return true, false
	}
	writeFixed(output.Screen, directory, sessionResult)
	return false, true
}

func sessionCandidates(directory string, requestedNames []string) ([]candidate, error) {
	if len(requestedNames) > 0 {
		candidates := make([]candidate, 0, len(requestedNames))
		seen := make(map[string]struct{}, len(requestedNames))
		for _, name := range requestedNames {
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
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
		case entry.Type().IsRegular() && strings.HasSuffix(entryName, session.ArchiveMetaCacheSuffix) &&
			session.IsArchived(directory, strings.TrimSuffix(entryName, session.ArchiveMetaCacheSuffix)):
			continue
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

func checkSessions(directory string, candidates []candidate) []result {
	results := make([]result, len(candidates))
	jobs := make(chan int, len(candidates))
	for index := range candidates {
		jobs <- index
	}
	close(jobs)

	var workers sync.WaitGroup
	workerCount := min(maxCheckWorkers, runtime.GOMAXPROCS(0), len(candidates))
	for range workerCount {
		workers.Go(func() {
			for index := range jobs {
				results[index] = inspectSession(directory, candidates[index])
			}
		})
	}
	workers.Wait()
	return results
}

func inspectSession(directory string, one candidate) result {
	sessionResult := result{candidate: one}
	if one.err != nil {
		sessionResult.state = invalid
		sessionResult.err = one.err
		return sessionResult
	}

	inspection, err := session.Inspect(directory, one.name)
	if err != nil {
		sessionResult.state = invalid
		sessionResult.err = err
		return sessionResult
	}
	if inspection.IsInUse {
		sessionResult.state = running
		return sessionResult
	}
	sessionResult.archived = inspection.IsArchived

	expectedMeta, err := store.ListingMetaOf(inspection.Session)
	if err != nil {
		sessionResult.state = invalid
		sessionResult.err = err
		return sessionResult
	}
	if inspection.HasIncompleteTail {
		sessionResult.repairs = append(sessionResult.repairs, repairTail)
	}
	if inspection.Session.HasIncompleteTurn {
		sessionResult.repairs = append(sessionResult.repairs, repairTurn)
	}
	if format.IsNewer(inspection.MetaError) {
		sessionResult.state = invalid
		sessionResult.err = inspection.MetaError
		return sessionResult
	}
	if inspection.MetaError != nil || !reflect.DeepEqual(inspection.Meta, expectedMeta) {
		sessionResult.repairs = append(sessionResult.repairs, repairMeta)
	}
	if !inspection.HasTranscript {
		sessionResult.repairs = append(sessionResult.repairs, repairTranscript)
	}

	switch {
	case len(sessionResult.repairs) == 0:
		sessionResult.state = healthy
	case len(sessionResult.repairs) == 1 && sessionResult.repairs[0] == repairTurn:
		sessionResult.state = recoverable
	default:
		sessionResult.state = repairable
	}
	return sessionResult
}

type journalStamp struct {
	size       int64
	modifiedAt time.Time
}

func fixSession(directory string, sessionResult result) error {
	if sessionResult.archived {
		return errors.New("restore the archived session before fixing it")
	}

	backupRoot := directory + "_pre_check"
	heldLock, err := session.AcquireLock(directory, sessionResult.candidate.name)
	if err != nil {
		return err
	}
	copyError := copyTree(
		session.Dir(directory, sessionResult.candidate.name),
		filepath.Join(backupRoot, sessionResult.candidate.name),
	)
	originalStamp, stampError := journalStampOf(directory, sessionResult.candidate.name)
	releaseError := heldLock.Release()
	if err := errors.Join(copyError, stampError, releaseError); err != nil {
		return fmt.Errorf("keep a copy: %w", err)
	}

	postRepairStamp, err := repairStoredSession(directory, sessionResult, originalStamp)
	if err != nil {
		return err
	}
	if slices.Contains(sessionResult.repairs, repairTurn) {
		_, err := store.AbandonIncompleteTurn(directory, sessionResult.candidate.name, func() error {
			return requireJournalStamp(directory, sessionResult.candidate.name, postRepairStamp)
		})
		if err != nil {
			return err
		}
	}

	result := inspectSession(directory, candidate{
		name: sessionResult.candidate.name, target: sessionResult.candidate.target,
	})
	if result.state != healthy {
		if result.err != nil {
			return result.err
		}
		if result.state == running {
			return errors.New("the session was opened while it was being repaired")
		}
		return fmt.Errorf("repair left %s", repairsText(result.repairs))
	}
	return nil
}

func repairStoredSession(directory string, sessionResult result, expectedStamp journalStamp) (journalStamp, error) {
	name := sessionResult.candidate.name
	heldLock, err := session.AcquireLock(directory, name)
	if err != nil {
		return journalStamp{}, err
	}
	if err := requireJournalStamp(directory, name, expectedStamp); err != nil {
		_ = heldLock.Release()
		return journalStamp{}, err
	}

	_, repairError := session.RepairJournalTail(directory, name)
	if repairError == nil && slices.Contains(sessionResult.repairs, repairMeta) {
		repairError = store.RebuildMeta(directory, name)
	}
	if repairError == nil && slices.Contains(sessionResult.repairs, repairTranscript) {
		repairError = store.Rebuild(directory, name)
	}
	stamp, stampError := journalStampOf(directory, name)
	releaseError := heldLock.Release()
	return stamp, errors.Join(repairError, stampError, releaseError)
}

func journalStampOf(directory string, name string) (journalStamp, error) {
	info, err := os.Stat(filepath.Join(session.Dir(directory, name), "session.jsonl"))
	if err != nil {
		return journalStamp{}, err
	}
	return journalStamp{size: info.Size(), modifiedAt: info.ModTime()}, nil
}

func requireJournalStamp(directory string, name string, expectedStamp journalStamp) error {
	actual, err := journalStampOf(directory, name)
	if err != nil {
		return err
	}
	if actual != expectedStamp {
		return errors.New("session changed while its repair started; no further repair was attempted")
	}
	return nil
}

func copyTree(source string, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("a copy is already kept in %s: move it aside first", destination)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		copyPath := filepath.Join(destination, relative)
		switch {
		case entry.IsDir():
			return os.MkdirAll(copyPath, 0o700)
		case entry.Type().IsRegular():
			return copyFile(path, copyPath)
		default:
			return fmt.Errorf("%s is neither a file nor a directory", path)
		}
	})
}

func copyFile(source string, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("a copy is already kept in %s: move it aside first", destination)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source) //nolint:gosec // the source belongs to the selected session
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // the destination is beneath the fixed backup directory
	if err != nil {
		return err
	}
	_, copyError := io.Copy(output, input)
	return errors.Join(copyError, output.Close())
}

func repairsText(repairs []repair) string {
	parts := make([]string, len(repairs))
	for index, one := range repairs {
		parts[index] = string(one)
	}
	return strings.Join(parts, ", ")
}

func writeWarning(writer io.Writer, target string, message string) {
	_, _ = fmt.Fprintln(writer, style.Warning("warning")+" "+target+": "+message)
}

func writeRepairable(writer io.Writer, sessionResult *result) {
	_, _ = fmt.Fprintln(writer, style.Warning("repairable")+" "+sessionResult.candidate.target+": "+repairsText(sessionResult.repairs))
}

func writePlanned(writer io.Writer, sessionResult *result) {
	_, _ = fmt.Fprintln(writer, style.Subtle("would fix")+" "+sessionResult.candidate.target+": "+repairsText(sessionResult.repairs))
}

func writeFixed(writer io.Writer, directory string, sessionResult *result) {
	backupPath := filepath.Join(directory+"_pre_check", sessionResult.candidate.name)
	_, _ = fmt.Fprintln(
		writer,
		style.Success("fixed")+" "+sessionResult.candidate.target+": "+repairsText(sessionResult.repairs)+
			style.Subtle("; copy kept in "+backupPath),
	)
}

func writeFailure(writer io.Writer, target string, err error) {
	_, _ = fmt.Fprintln(writer, style.Failure("failed")+" "+target+": "+err.Error())
}
