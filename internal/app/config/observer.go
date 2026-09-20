package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/util"
)

type snapshot struct {
	data      []byte
	failure   error
	isMissing bool
}

const readableBytes = 256 << 10

func readSnapshot(path string) snapshot {
	data, err := readWrittenFile(path)
	return snapshot{data: data, failure: err, isMissing: errors.Is(err, fs.ErrNotExist)}
}

func readWrittenFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0) //nolint:gosec // the path is the configured one
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected normal file, not %s", namedFileKind(info.Mode()))
	}

	data, err := io.ReadAll(io.LimitReader(file, readableBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > readableBytes {
		return nil, fmt.Errorf(
			"is larger than %s, which is more than a config is ever written by hand",
			util.FormatBytes(readableBytes, 0),
		)
	}

	return data, nil
}

func namedFileKind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeDevice != 0:
		return "a device"
	default:
		return "not an ordinary file"
	}
}

func (self snapshot) equal(other snapshot) bool {
	return self.isMissing == other.isMissing &&
		bytes.Equal(self.data, other.data) &&
		errorText(self.failure) == errorText(other.failure)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type revision struct {
	sourceSnapshots      []sourceSnapshot
	snippetFileSnapshots map[string]snapshot
	snippetSettings      map[string][]string
}

func (self revision) equal(other revision) bool {
	if len(self.sourceSnapshots) != len(other.sourceSnapshots) ||
		len(self.snippetFileSnapshots) != len(other.snippetFileSnapshots) {
		return false
	}
	for i, source := range self.sourceSnapshots {
		otherSource := other.sourceSnapshots[i]
		if source.source != otherSource.source || !source.snapshot.equal(otherSource.snapshot) {
			return false
		}
	}
	for path, current := range self.snippetFileSnapshots {
		previous, exists := other.snippetFileSnapshots[path]
		if !exists || !current.equal(previous) {
			return false
		}
	}
	return true
}

func (self revision) changesSince(previous revision) []SourceChange {
	changes := make([]SourceChange, 0, len(self.sourceSnapshots))
	isNamed := map[string]bool{}

	for i, source := range self.sourceSnapshots {
		var was snapshot
		if i < len(previous.sourceSnapshots) {
			was = previous.sourceSnapshots[i].snapshot
			if source.snapshot.equal(was) {
				continue
			}
		}
		settings := changedSettings(was, source.snapshot)
		for _, setting := range settings {
			isNamed[setting] = true
		}
		changes = append(changes, SourceChange{
			Path:      filepath.Base(source.source.Path),
			Settings:  settings,
			IsRemoved: source.snapshot.isMissing,
		})
	}

	for _, path := range slices.Sorted(maps.Keys(self.snippetFileSnapshots)) {
		current := self.snippetFileSnapshots[path]
		if was, isKnown := previous.snippetFileSnapshots[path]; isKnown && current.equal(was) {
			continue
		}
		settings := self.snippetSettings[path]
		if isEverySettingNamed(settings, isNamed) {
			continue
		}
		changes = append(changes, SourceChange{
			Path:      filepath.Base(path),
			Settings:  settings,
			IsRemoved: current.isMissing,
		})
	}

	return changes
}

func isEverySettingNamed(settings []string, isNamed map[string]bool) bool {
	if len(settings) == 0 {
		return false
	}
	for _, setting := range settings {
		if !isNamed[setting] {
			return false
		}
	}
	return true
}

func changedSettings(previous snapshot, current snapshot) []string {
	was, isPreviousRead := flattenSettings(previous)
	now, isCurrentRead := flattenSettings(current)
	if !isPreviousRead || !isCurrentRead {
		return nil
	}

	names := make(map[string]struct{})
	for name, value := range now {
		if !reflect.DeepEqual(was[name], value) {
			names[name] = struct{}{}
		}
	}
	for name := range was {
		if _, isKept := now[name]; !isKept {
			names[name] = struct{}{}
		}
	}

	return slices.Sorted(maps.Keys(names))
}

func flattenSettings(source snapshot) (map[string]any, bool) {
	if source.isMissing {
		return map[string]any{}, true
	}
	if source.failure != nil {
		return nil, false
	}

	var tables map[string]any
	if _, err := toml.Decode(string(source.data), &tables); err != nil {
		return nil, false
	}

	settings := make(map[string]any)
	flatten(settings, nil, tables)
	delete(settings, versionSetting)

	return settings, true
}

func flatten(into map[string]any, prefix []string, values map[string]any) {
	for name, value := range values {
		path := append(slices.Clone(prefix), name)
		table, isTable := value.(map[string]any)
		if isTable && len(table) > 0 && !isLeafTable(path) {
			flatten(into, path, table)
			continue
		}
		into[strings.Join(path, ".")] = value
	}
}

func isLeafTable(path []string) bool {
	if len(path) != 2 {
		return false
	}
	return path[0] == snippetsSetting || (path[0] == uiSetting && path[1] == themeSetting)
}

func leafTableOf(key toml.Key) toml.Key {
	for depth := 1; depth < len(key); depth++ {
		if isLeafTable(key[:depth]) {
			return key[:depth]
		}
	}
	return key
}

func (self revision) getPaths() []string {
	paths := make([]string, 0, len(self.sourceSnapshots)+len(self.snippetFileSnapshots))
	for _, source := range self.sourceSnapshots {
		paths = append(paths, source.source.Path)
	}
	return append(paths, slices.Sorted(maps.Keys(self.snippetFileSnapshots))...)
}

func readRevision(sources []Source) (Config, revision, error) {
	snapshots := make([]sourceSnapshot, 0, len(sources))
	for _, source := range sources {
		snapshots = append(snapshots, sourceSnapshot{source: source, snapshot: readSnapshot(source.Path)})
	}
	settings, err := loadSnapshots(snapshots)
	return settings, revision{
		sourceSnapshots:      snapshots,
		snippetFileSnapshots: settings.snippetFileSnapshots,
		snippetSettings:      snippetSettings(settings),
	}, err
}

func snippetSettings(settings Config) map[string][]string {
	names := make(map[string][]string, len(settings.snippetFileSnapshots))
	for _, name := range slices.Sorted(maps.Keys(settings.Snippets)) {
		if path := settings.Snippets[name].File; path != "" {
			names[path] = append(names[path], snippetsSetting+"."+name)
		}
	}
	return names
}

type Observer struct {
	sources         []Source
	handledRevision revision
	watcher         *fileWatcher
}

func Observe(path string) (Config, *Observer, error) {
	return ObserveSources(Source{Path: path})
}

func ObserveSources(sources ...Source) (Config, *Observer, error) {
	settings, current, err := readRevision(sources)
	if err != nil {
		return Config{}, nil, err
	}

	watcher, err := newFileWatcher(current.getPaths()...)
	if err != nil {
		return Config{}, nil, fmt.Errorf("could not watch config: %w", err)
	}

	latestSettings, latest, err := readRevision(sources)
	if err == nil {
		err = watcher.addPaths(latest.getPaths()...)
	}
	if err != nil {
		watcher.close()
		return Config{}, nil, err
	}
	if !latest.equal(current) {
		settings = latestSettings
		current = latest
	}

	return settings, &Observer{sources: slices.Clone(sources), handledRevision: current, watcher: watcher}, nil
}

type ReloadStatus int

const (
	ReloadUnchanged ReloadStatus = iota
	ReloadApplied
	ReloadFailed
)

type SourceChange struct {
	Path      string
	Settings  []string
	IsRemoved bool
}

type ReloadResult struct {
	LiveConfig LiveConfig
	Changes    []SourceChange
	Status     ReloadStatus
	Failure    error
}

func (self *Observer) Changes() <-chan error {
	if self == nil {
		return nil
	}
	return self.watcher.events
}

func (self *Observer) Reload(watchFailure error, registry segment.Registry) ReloadResult {
	settings, changes, err := self.refresh(watchFailure)
	if err == nil && len(changes) > 0 {
		var live LiveConfig
		live, err = settings.BuildLive(registry)
		if err == nil {
			return ReloadResult{LiveConfig: live, Changes: changes, Status: ReloadApplied}
		}
	}
	if err != nil {
		return ReloadResult{Status: ReloadFailed, Failure: err}
	}
	return ReloadResult{Status: ReloadUnchanged}
}

func (self *Observer) Close() {
	if self != nil {
		self.watcher.close()
	}
}

func (self *Observer) refresh(watchFailure error) (Config, []SourceChange, error) {
	if self == nil {
		return Config{}, nil, nil
	}
	if watchFailure != nil {
		return Config{}, nil, fmt.Errorf("could not watch config: %w", watchFailure)
	}

	settings, current, err := readRevision(self.sources)
	if watchErr := self.watcher.addPaths(current.getPaths()...); watchErr != nil {
		return Config{}, nil, fmt.Errorf("could not watch config: %w", watchErr)
	}

	latestSettings, latest, latestErr := readRevision(self.sources)
	if watchErr := self.watcher.addPaths(latest.getPaths()...); watchErr != nil {
		return Config{}, nil, fmt.Errorf("could not watch config: %w", watchErr)
	}
	if !latest.equal(current) {
		settings = latestSettings
		current = latest
		err = latestErr
	}

	if current.equal(self.handledRevision) {
		return Config{}, nil, nil
	}
	previous := self.handledRevision
	self.handledRevision = current
	if err != nil {
		return Config{}, nil, err
	}
	return settings, current.changesSince(previous), nil
}

const watchMask = unix.IN_ATTRIB |
	unix.IN_CLOSE_WRITE |
	unix.IN_CREATE |
	unix.IN_DELETE |
	unix.IN_DELETE_SELF |
	unix.IN_MOVE_SELF |
	unix.IN_MOVED_FROM |
	unix.IN_MOVED_TO

type fileWatcher struct {
	targetPaths      map[string]struct{}
	watchDescriptors map[int]struct{}

	inotifyDescriptor int
	cancelRead        int
	cancelWrite       int

	events        chan error
	closedChannel chan struct{}
	mutex         sync.Mutex
	once          sync.Once
}

func newFileWatcher(paths ...string) (*fileWatcher, error) {
	inotifyDescriptor, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}

	cancel := []int{0, 0}
	if err := unix.Pipe2(cancel, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		_ = unix.Close(inotifyDescriptor)
		return nil, err
	}

	watcher := &fileWatcher{
		targetPaths:       make(map[string]struct{}),
		watchDescriptors:  make(map[int]struct{}),
		inotifyDescriptor: inotifyDescriptor,
		cancelRead:        cancel[0],
		cancelWrite:       cancel[1],
		events:            make(chan error, 1),
		closedChannel:     make(chan struct{}),
	}
	if err := watcher.addPaths(paths...); err != nil {
		watcher.closeDescriptors()
		return nil, err
	}

	go watcher.run()
	return watcher, nil
}

func (self *fileWatcher) addPaths(paths ...string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, path := range paths {
		self.targetPaths[filepath.Clean(path)] = struct{}{}
	}
	return self.armLocked()
}

func (self *fileWatcher) arm() error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.armLocked()
}

func (self *fileWatcher) armLocked() error {
	directories := make(map[string]struct{}, len(self.targetPaths))
	for path := range self.targetPaths {
		directory, err := deepestExistingDirectory(filepath.Dir(path))
		if err != nil {
			return err
		}
		directories[directory] = struct{}{}
	}
	for directory := range directories {
		descriptor, err := unix.InotifyAddWatch(self.inotifyDescriptor, directory, watchMask)
		if err != nil {
			return err
		}
		self.watchDescriptors[descriptor] = struct{}{}
	}
	return nil
}

func deepestExistingDirectory(path string) (string, error) {
	for {
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%s is not a directory", path)
			}
			return path, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

func (self *fileWatcher) run() {
	defer close(self.closedChannel)
	defer close(self.events)
	defer self.closeDescriptors()

	pollDescriptors := []unix.PollFd{
		//nolint:gosec // poll uses the kernel's signed 32-bit file descriptors
		{Fd: int32(self.inotifyDescriptor), Events: unix.POLLIN},
		//nolint:gosec // poll uses the kernel's signed 32-bit file descriptors
		{Fd: int32(self.cancelRead), Events: unix.POLLIN},
	}
	buffer := make([]byte, unix.SizeofInotifyEvent*64)
	for {
		_, err := unix.Poll(pollDescriptors, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			self.publishFailure(err)
			return
		}
		if pollDescriptors[1].Revents&unix.POLLIN != 0 {
			return
		}
		if pollDescriptors[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			self.publishFailure(errors.New("inotify stopped unexpectedly"))
			return
		}
		if pollDescriptors[0].Revents&unix.POLLIN == 0 {
			continue
		}

		if _, err := unix.Read(self.inotifyDescriptor, buffer); err != nil && !errors.Is(err, unix.EAGAIN) {
			self.publishFailure(err)
			return
		}
		if err := self.arm(); err != nil {
			self.publishFailure(err)
			return
		}
		self.publishChange()
	}
}

func (self *fileWatcher) publishChange() {
	select {
	case self.events <- nil:
	default:
	}
}

func (self *fileWatcher) publishFailure(err error) {
	select {
	case <-self.events:
	default:
	}
	self.events <- err
}

func (self *fileWatcher) close() {
	self.once.Do(func() {
		_, _ = unix.Write(self.cancelWrite, []byte{0})
		<-self.closedChannel
	})
}

func (self *fileWatcher) closeDescriptors() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for descriptor := range self.watchDescriptors {
		//nolint:gosec // inotify returns its watch descriptors as non-negative ints
		_, _ = unix.InotifyRmWatch(self.inotifyDescriptor, uint32(descriptor))
	}
	_ = unix.Close(self.inotifyDescriptor)
	_ = unix.Close(self.cancelRead)
	_ = unix.Close(self.cancelWrite)
}
