package gc

import (
	"cmp"
	"debug/buildinfo"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"crdx.org/duckopt/v2"

	"crdx.org/io/internal/app/ctl/console"
	"crdx.org/io/internal/app/location"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/table"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/session"
)

const usage = `oh --ctl gc — remove what sessions leave behind

Usage:
    $0 --ctl gc [options]

Options:
    -a, --aggressive    Look through the whole of each directory, and delete rebuildable Go binaries
    -n, --dry-run       Report what would be removed without removing it
    -h, --help          Show this help
`

const (
	cacheName      = ".cache"
	bytePrecision  = 3
	farmLabel      = "farm"
	homeLabel      = "home"
	sweepsPerCPU   = 4
	minimumSweeps  = 32
	ownerPerm      = 0o700
	blockBytes     = 512
	executablePerm = 0o111
)

const (
	buildTrimName   = "trim.txt"
	buildReadmeName = "README"
	firstBucketName = "00"
	lastBucketName  = "ff"
	downloadName    = "download"
	moduleCacheName = "cache"
	domainSeparator = "."
	moduleFileName  = "go.mod"
	modulePrefix    = "module "
)

const (
	namedCacheKind  = ".cache"
	buildWorkKind   = "go-build-work"
	buildCacheKind  = "go-build-cache"
	moduleCacheKind = "go-module-cache"
	abandonedKind   = "no-session"
	binaryKind      = "go-binary"
)

var (
	buildWorkPattern = regexp.MustCompile(`^go-build[0-9]*$`)
	buildStepPattern = regexp.MustCompile(`^b[0-9]+$`)
)

type Directories struct {
	Farm     string
	Sessions string
	Home     string
}

type root struct {
	path  string
	label string
	kind  string
}

type cache struct {
	path string
	name string
	kind string
}

type removal struct {
	name  string
	kind  string
	bytes int64
}

type result struct {
	removals []removal
	failures []string
}

type inputOpts struct {
	IsControlling bool `docopt:"--ctl"`
	GC            bool `docopt:"gc"`
	DryRun        bool `docopt:"--dry-run"`
	Aggressive    bool `docopt:"--aggressive"`
}

type options struct {
	isDryRun     bool
	isAggressive bool
}

func Run() error {
	bound := duckopt.MustBind[inputOpts](usage, "$0")

	directories := Directories{
		Farm:     location.GetFarmDir(),
		Sessions: location.GetSessionsDir(),
		Home:     location.GetShellHomeDir(),
	}

	return run(directories, options{
		isDryRun:     bound.DryRun,
		isAggressive: bound.Aggressive,
	}, console.Standard())
}

func run(directories Directories, choice options, output console.Output) error {
	roots, runningCount, err := collect(directories)
	if err != nil {
		return err
	}

	total := sweep(roots, choice)

	slices.Sort(total.failures)
	for _, failure := range total.failures {
		_, _ = fmt.Fprintln(output.Failure, style.Failure(failure))
	}

	slices.SortFunc(total.removals, byLargest)
	writeRemovals(total.removals, output.Screen)

	count := len(total.removals)
	_, _ = fmt.Fprintln(output.Screen, style.Subtle(
		summary(count, reclaimedBytes(total.removals), runningCount, choice.isDryRun),
	))

	if failureCount := len(total.failures); failureCount > 0 {
		return fmt.Errorf("%d of %d could not be removed", failureCount, count+failureCount)
	}

	return nil
}

func byLargest(one removal, other removal) int {
	if difference := cmp.Compare(other.bytes, one.bytes); difference != 0 {
		return difference
	}

	return strings.Compare(one.name, other.name)
}

func reclaimedBytes(removals []removal) int64 {
	var totalBytes int64
	for _, one := range removals {
		totalBytes += one.bytes
	}

	return totalBytes
}

func writeRemovals(removals []removal, writer io.Writer) {
	if len(removals) == 0 {
		return
	}

	rows := make([][]string, len(removals))
	for index, one := range removals {
		rows[index] = []string{one.kind, util.FormatBytes(one.bytes, bytePrecision), one.name}
	}

	removalTable := table.New(
		table.Column{Title: "Kind", Style: style.Qualifier},
		table.Column{Title: "Size", Align: table.Right},
		table.Column{Title: "Path"},
	).Fit(rows)

	_, _ = fmt.Fprintln(writer, style.Column(removalTable.Header(0)))
	for _, cells := range rows {
		_, _ = fmt.Fprintln(writer, removalTable.Row(cells, 0))
	}
	_, _ = fmt.Fprintln(writer)
}

func collect(directories Directories) ([]root, int, error) {
	entries, err := os.ReadDir(directories.Farm)
	if err != nil && !os.IsNotExist(err) {
		return nil, 0, err
	}

	roots := make([]root, 0, len(entries)+1)
	runningCount := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		isRunning, err := session.IsInUse(directories.Sessions, entry.Name())
		if err == nil && isRunning {
			runningCount++
			continue
		}

		roots = append(roots, root{
			path:  filepath.Join(directories.Farm, entry.Name()),
			label: filepath.Join(farmLabel, entry.Name()),
			kind:  wholeKind(directories.Sessions, entry.Name()),
		})
	}

	if runningCount == 0 {
		roots = append(roots, root{path: directories.Home, label: homeLabel})
	}

	return roots, runningCount, nil
}

func wholeKind(sessions string, name string) string {
	if session.IsName(name) && !session.Exists(sessions, name) {
		return abandonedKind
	}

	return ""
}

func sweep(roots []root, choice options) result {
	queue := make(chan root)
	results := make(chan result)

	var sweepers sync.WaitGroup
	for range min(max(runtime.NumCPU()*sweepsPerCPU, minimumSweeps), len(roots)) {
		sweepers.Go(func() {
			for next := range queue {
				results <- take(next, choice)
			}
		})
	}

	go func() {
		for _, next := range roots {
			queue <- next
		}
		close(queue)
	}()

	go func() {
		sweepers.Wait()
		close(results)
	}()

	var total result
	for one := range results {
		total.removals = append(total.removals, one.removals...)
		total.failures = append(total.failures, one.failures...)
	}

	return total
}

func take(next root, choice options) result {
	var outcome result

	caches, err := find(next, choice.isAggressive)
	if err != nil {
		outcome.failures = append(outcome.failures, next.label+": "+err.Error())
	}

	for _, found := range caches {
		removedBytes, err := size(found.path)
		if err == nil && !choice.isDryRun {
			err = remove(found.path)
		}
		if err != nil {
			outcome.failures = append(outcome.failures, found.name+": "+err.Error())
			continue
		}

		outcome.removals = append(outcome.removals, removal{
			name:  found.name,
			kind:  found.kind,
			bytes: removedBytes,
		})
	}

	return outcome
}

func remove(path string) error {
	err := os.RemoveAll(path)
	if err == nil || !os.IsPermission(err) {
		return err
	}

	if err := unlock(path); err != nil {
		return err
	}

	return os.RemoveAll(path)
}

func unlock(root string) error {
	var lockedDirectories []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}
		if !entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return skip(entry)
		}
		if info.Mode().Perm()&ownerPerm != ownerPerm {
			lockedDirectories = append(lockedDirectories, path)
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, path := range lockedDirectories {
		if err := os.Chmod(path, ownerPerm); err != nil {
			return err
		}
	}

	return nil
}

func find(next root, isAggressive bool) ([]cache, error) {
	if next.kind != "" {
		return []cache{{path: next.path, name: next.label, kind: next.kind}}, nil
	}

	entries, err := os.ReadDir(next.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	hunt := search{root: next, isAggressive: isAggressive, modules: map[string]string{}}
	if err := hunt.gather(next.path, entries); err != nil {
		return hunt.caches, err
	}

	return hunt.caches, nil
}

type search struct {
	root         root
	isAggressive bool
	caches       []cache
	modules      map[string]string
}

func (self *search) gather(path string, entries []os.DirEntry) error {
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())

		if !entry.IsDir() {
			if self.isAggressive && self.isRebuildable(child, entry) {
				if err := self.keep(child, binaryKind); err != nil {
					return err
				}
			}

			continue
		}

		childEntries, err := os.ReadDir(child)
		if err != nil {
			if entry.Name() == cacheName {
				if err := self.keep(child, namedCacheKind); err != nil {
					return err
				}
			}

			continue
		}

		if kind := cacheKind(child, entry.Name(), childEntries); kind != "" {
			if err := self.keep(child, kind); err != nil {
				return err
			}
			continue
		}

		if self.isAggressive {
			if err := self.gather(child, childEntries); err != nil {
				return err
			}
		}
	}

	return nil
}

func (self *search) keep(path string, kind string) error {
	name, err := filepath.Rel(self.root.path, path)
	if err != nil {
		return err
	}
	self.caches = append(self.caches, cache{
		path: path,
		name: filepath.Join(self.root.label, name),
		kind: kind,
	})

	return nil
}

func (self *search) isRebuildable(path string, entry os.DirEntry) bool {
	info, err := entry.Info()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&executablePerm == 0 {
		return false
	}

	information, err := buildinfo.ReadFile(path)
	if err != nil {
		return false
	}

	module := self.moduleAbove(filepath.Dir(path))

	return module != "" && module == information.Main.Path
}

func (self *search) moduleAbove(directory string) string {
	if module, isKnown := self.modules[directory]; isKnown {
		return module
	}

	module := declaredModule(filepath.Join(directory, moduleFileName))
	if module == "" && directory != self.root.path && strings.HasPrefix(directory, self.root.path) {
		module = self.moduleAbove(filepath.Dir(directory))
	}
	self.modules[directory] = module

	return module
}

func declaredModule(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // a path beneath a root the sweep already reached
	if err != nil {
		return ""
	}

	for line := range strings.Lines(string(data)) {
		if after, isDeclaration := strings.CutPrefix(strings.TrimSpace(line), modulePrefix); isDeclaration {
			return strings.Trim(strings.TrimSpace(after), `"`)
		}
	}

	return ""
}

func cacheKind(path string, name string, entries []os.DirEntry) string {
	if name == cacheName {
		return namedCacheKind
	}

	contents := make(map[string]bool, len(entries))
	for _, entry := range entries {
		contents[entry.Name()] = entry.IsDir()
	}

	switch {
	case holdsBuildWork(name, contents):
		return buildWorkKind
	case holdsBuildCache(contents):
		return buildCacheKind
	case holdsModuleCache(path, contents):
		return moduleCacheKind
	}

	return ""
}

func holdsBuildWork(name string, contents map[string]bool) bool {
	if !buildWorkPattern.MatchString(name) {
		return false
	}

	for entry, isDirectory := range contents {
		if !isDirectory || !buildStepPattern.MatchString(entry) {
			return false
		}
	}

	return true
}

func holdsBuildCache(contents map[string]bool) bool {
	if !contents[firstBucketName] || !contents[lastBucketName] {
		return false
	}

	return isMarker(contents, buildTrimName) || isMarker(contents, buildReadmeName)
}

func holdsModuleCache(path string, contents map[string]bool) bool {
	if !contents[moduleCacheName] {
		return false
	}

	downloads, err := os.ReadDir(filepath.Join(path, moduleCacheName, downloadName))
	if err != nil {
		return false
	}

	for _, entry := range downloads {
		if entry.IsDir() && strings.Contains(entry.Name(), domainSeparator) {
			return true
		}
	}

	return false
}

func isMarker(contents map[string]bool, name string) bool {
	isDirectory, isPresent := contents[name]

	return isPresent && !isDirectory
}

func takenNoun(count int) string {
	return util.PluralNoun(count, "path")
}

func summary(count int, reclaimedBytes int64, runningCount int, isDryRun bool) string {
	text := strconv.Itoa(count) + " " + takenNoun(count) + ", " +
		util.FormatBytes(reclaimedBytes, bytePrecision)
	if isDryRun {
		text += " to reclaim"
	} else {
		text += " reclaimed"
	}

	if runningCount > 0 {
		text += ", " + util.Plural(runningCount, "running session") + " and the shared home left alone"
	}

	return text
}

func skip(entry fs.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return fs.SkipDir
	}

	return nil
}

func size(root string) (int64, error) {
	var totalBytes int64

	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}

		info, err := entry.Info()
		if err != nil {
			return skip(entry)
		}
		totalBytes += occupiedBytes(info)

		return nil
	})

	return totalBytes, err
}

func occupiedBytes(info fs.FileInfo) int64 {
	stat, isSystem := info.Sys().(*syscall.Stat_t)
	if !isSystem || !info.Mode().IsRegular() {
		return 0
	}

	return stat.Blocks * blockBytes
}
