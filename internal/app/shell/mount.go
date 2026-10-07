package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
)

type configuredMount struct {
	directory string
	target    string
	name      string
	isExact   bool
}

type mountedPath struct {
	mount *configuredMount
	root  *file.Root
}

type PathAccess struct {
	files      *file.Root
	mode       *caps.Mode
	searchDeny sandbox.DenySearch

	mutex            sync.RWMutex
	configuredPaths  Paths
	baselineMounts   map[string]mountedPath
	configuredMounts map[string]mountedPath
	temporaryMounts  map[string]mountedPath
	temporaryAccess  map[string]Access
	denyPathCache    map[string][]string
	denySearches     map[string]*pendingDenySearch
	denials          pathDenials
	roots            []*file.Root
	skillDirectories []string
}

type pendingDenySearch struct {
	over  chan struct{}
	paths []string
	err   error
}

func NewPathAccess(files *file.Root, mode *caps.Mode, paths Paths) (*PathAccess, error) {
	denials, err := newPathDenials(paths.Deny)
	if err != nil {
		return nil, err
	}
	files.SetAccessRefusal(denials.Refuse)
	files.SetExcludedNames(paths.Deny)

	access := &PathAccess{
		files:            files,
		mode:             mode,
		searchDeny:       sandbox.FindDenyPaths,
		configuredPaths:  clonePaths(paths),
		baselineMounts:   make(map[string]mountedPath),
		configuredMounts: make(map[string]mountedPath),
		temporaryMounts:  make(map[string]mountedPath),
		temporaryAccess:  make(map[string]Access),
		denyPathCache:    make(map[string][]string),
		denySearches:     make(map[string]*pendingDenySearch),
		denials:          denials,
	}

	for _, path := range implicitReadablePaths(files.Name(), paths.Write) {
		baselineMount, err := access.openBaselineMountedPath(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			access.Close()
			return nil, fmt.Errorf("could not mount baseline path %s: %w", path, err)
		}
		access.baselineMounts[path] = baselineMount
		access.install(path, baselineMount)
	}

	for _, grant := range paths.ConfiguredGrants() {
		configuredPathMount, err := access.openMountedPath(grant.Path, grant.Access)
		if err != nil {
			access.Close()
			return nil, fmt.Errorf("could not mount configured path %s: %w", pathutil.Shorten(grant.Path), err)
		}
		access.configuredMounts[grant.Path] = configuredPathMount
		access.install(grant.Path, configuredPathMount)
	}

	return access, nil
}

func (self *PathAccess) ShareSkills(directories []string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.skillDirectories = nil
	for _, directory := range directories {
		self.skillDirectories = append(self.skillDirectories, pathutil.Canonicalise(directory))
	}
}

func (self *PathAccess) GetPaths() Paths {
	paths, _ := self.getPaths()
	return paths
}

func (self *PathAccess) Grant(path string, access Access) (bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path = filepath.Clean(path)
	if err := self.denials.Refuse(path); err != nil {
		return false, err
	}
	if current, exists := self.temporaryAccess[path]; exists && current == access {
		return false, nil
	}

	temporaryPathMount, err := self.openMountedPath(path, access)
	if err != nil {
		return false, err
	}
	if err := temporaryPathMount.root.Prepare(); err != nil {
		_ = temporaryPathMount.root.Close()
		return false, err
	}
	self.releaseTemporaryMount(path)
	self.temporaryMounts[path] = temporaryPathMount
	self.temporaryAccess[path] = access
	self.install(path, temporaryPathMount)
	return true, nil
}

func (self *PathAccess) Revoke(path string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path = filepath.Clean(path)
	if _, exists := self.temporaryAccess[path]; !exists {
		return false
	}
	delete(self.temporaryAccess, path)
	self.releaseTemporaryMount(path)
	return true
}

func (self *PathAccess) Close() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	closeRoots(self.roots)
	self.roots = nil
}

func (self *PathAccess) getDenyPaths(policy sandbox.Policy) ([]string, error) {
	return policy.DiscoverDenyPathsWith(self.searchDenyPaths)
}

func (self *PathAccess) searchDenyPaths(patterns []string, root string) ([]string, error) {
	key := denySearchKey(patterns, root)

	self.mutex.Lock()
	if paths, isSearched := self.denyPathCache[key]; isSearched {
		self.mutex.Unlock()
		return slices.Clone(paths), nil
	}
	if sharedSearch, isRunning := self.denySearches[key]; isRunning {
		self.mutex.Unlock()
		<-sharedSearch.over
		return slices.Clone(sharedSearch.paths), sharedSearch.err
	}
	search := &pendingDenySearch{over: make(chan struct{})}
	self.denySearches[key] = search
	self.mutex.Unlock()

	search.paths, search.err = self.searchDeny(patterns, root)

	self.mutex.Lock()
	delete(self.denySearches, key)
	if search.err == nil {
		self.denyPathCache[key] = search.paths
	}
	self.mutex.Unlock()
	close(search.over)

	return slices.Clone(search.paths), search.err
}

func denySearchKey(patterns []string, root string) string {
	sortedPatterns := slices.Sorted(slices.Values(patterns))
	return strings.Join(sortedPatterns, "\x00") + "\x01" + root
}

func (self *PathAccess) getPaths() (Paths, []string) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	paths := clonePaths(self.configuredPaths)
	for _, directory := range self.skillDirectories {
		if !slices.Contains(paths.Read, directory) {
			paths.Read = append(paths.Read, directory)
		}
		if !slices.Contains(paths.Exec, directory) {
			paths.Exec = append(paths.Exec, directory)
		}
	}
	var temporaryPaths []string
	for _, path := range slices.Sorted(maps.Keys(self.temporaryAccess)) {
		access := self.temporaryAccess[path]
		switch {
		case access.Has(WriteAccess):
			paths.Write = append(paths.Write, path)
		default:
			paths.Read = append(paths.Read, path)
		}
		paths.Exec = append(paths.Exec, path)
		if !pathsContain(self.configuredPaths, path) {
			temporaryPaths = append(temporaryPaths, path)
		}
	}
	return paths, temporaryPaths
}

func (self *PathAccess) releaseTemporaryMount(path string) {
	temporaryPathMount, exists := self.temporaryMounts[path]
	if !exists {
		return
	}
	delete(self.temporaryMounts, path)

	if configuredPathMount, exists := self.configuredMounts[path]; exists {
		self.install(path, configuredPathMount)
	} else if baselineMount, exists := self.baselineMounts[path]; exists {
		self.install(path, baselineMount)
	} else {
		self.files.Unmount(path)
	}
	_ = temporaryPathMount.root.Close()
}

func (self *PathAccess) openMountedPath(path string, access Access) (mountedPath, error) {
	mount, err := findConfiguredMount(path)
	if err != nil {
		return mountedPath{}, err
	}
	return self.mountLazily(mount, access), nil
}

func (self *PathAccess) mountLazily(mount configuredMount, access Access) mountedPath {
	root := newMountedRoot(self.mode, mount, access)
	self.roots = append(self.roots, root)
	return mountedPath{mount: &mount, root: root}
}

func (self *PathAccess) openBaselineMountedPath(path string) (mountedPath, error) {
	mount, err := findConfiguredMount(path)
	if err != nil {
		return mountedPath{}, err
	}
	if !mount.isExact {
		return self.mountLazily(mount, ReadAccess), nil
	}

	directoryError := probeOpening(mount.directory)
	if directoryError == nil {
		return self.mountLazily(mount, ReadAccess), nil
	}
	if !errors.Is(directoryError, fs.ErrPermission) {
		return mountedPath{}, directoryError
	}

	if err := probeOpening(mount.target); err != nil {
		return mountedPath{}, err
	}
	root := file.NewExactFile(mount.target, func(string) error { return file.ErrReadOnly })
	return mountedPath{mount: &mount, root: root}, nil
}

func (self *PathAccess) install(path string, pathMount mountedPath) {
	if pathMount.mount.isExact {
		self.files.MountFile(path, pathMount.root, pathMount.mount.name)
	} else {
		self.files.Mount(path, pathMount.root)
	}
}

func clonePaths(paths Paths) Paths {
	return Paths{
		Deny:  slices.Clone(paths.Deny),
		Read:  slices.Clone(paths.Read),
		Write: slices.Clone(paths.Write),
		Exec:  slices.Clone(paths.Exec),
		Path:  slices.Clone(paths.Path),
		Home:  slices.Clone(paths.Home),
	}
}

func pathsContain(paths Paths, path string) bool {
	return slices.Contains(paths.Read, path) ||
		slices.Contains(paths.Write, path) ||
		slices.Contains(paths.Exec, path) ||
		slices.Contains(paths.Path, path) ||
		slices.Contains(paths.Home, path)
}

func warnAboutCoveredPaths(paths Paths, warnings io.Writer) {
	coveringLists := []struct {
		name  string
		paths []string
	}{
		{"sandbox.write", paths.Write},
		{"sandbox.exec", paths.Exec},
		{"sandbox.path", paths.Path},
	}

	for _, path := range paths.Read {
		for _, coveringList := range coveringLists {
			if !slices.Contains(coveringList.paths, path) {
				continue
			}

			style.WriteWarningf(
				warnings,
				"configured path %s is redundant in [sandbox.read] due to %s",
				pathutil.Shorten(path),
				coveringList.name,
			)
		}
	}

	warnAboutContainedPaths(paths, warnings)
}

type containment struct {
	outer  []string
	inner  []string
	clause string
	source string
}

func warnAboutContainedPaths(paths Paths, warnings io.Writer) {
	for _, pair := range []containment{
		{paths.Write, paths.Read, "is writable but holds the read-only", "sandbox.read"},
		{paths.Exec, paths.Read, "is executable but holds the read-only", "sandbox.read"},
		{paths.Path, paths.Read, "is executable but holds the read-only", "sandbox.read"},
	} {
		reportContainment(warnings, pair)
	}
}

func reportContainment(warnings io.Writer, pair containment) {
	for _, above := range pair.outer {
		for _, below := range pair.inner {
			if above == below {
				continue
			}
			if _, isBelow := pathutil.RelativeTo(above, below); !isBelow {
				continue
			}

			style.WriteWarningf(
				warnings,
				"configured path %s %s %s from [%s]",
				pathutil.Shorten(above),
				pair.clause,
				pathutil.Shorten(below),
				pair.source,
			)
		}
	}
}

func PreparePaths(paths Paths, warnings io.Writer) (Paths, error) {
	denials, err := newPathDenials(paths.Deny)
	if err != nil {
		return Paths{}, err
	}

	filteredPaths := Paths{Deny: slices.Clone(denials.patterns)}
	lists := []struct {
		source  []string
		target  *[]string
		isGrant bool
	}{
		{paths.Read, &filteredPaths.Read, true},
		{paths.Write, &filteredPaths.Write, true},
		{paths.Exec, &filteredPaths.Exec, true},
		{paths.Path, &filteredPaths.Path, true},
		{paths.Home, &filteredPaths.Home, false},
	}

	for _, list := range lists {
		for _, path := range list.source {
			_, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) {
				if err := os.MkdirAll(path, 0o700); err != nil {
					style.WriteWarningf(
						warnings,
						"could not create configured path %s: %v",
						pathutil.Shorten(path),
						err,
					)
					continue
				}
			} else if err != nil {
				return Paths{}, fmt.Errorf(
					"could not mount configured path %s: %w",
					pathutil.Shorten(path),
					err,
				)
			}
			if list.isGrant {
				path = pathutil.Canonicalise(path)
			}
			*list.target = append(*list.target, path)
		}
	}

	warnAboutCoveredPaths(filteredPaths, warnings)

	return filteredPaths, nil
}

func implicitReadablePaths(workspaceDir string, writableRoots []string) []string {
	workspaceRoot := pathutil.Canonicalise(workspaceDir)
	seen := make(map[string]struct{})
	var paths []string

	for _, path := range slices.Concat(sandbox.BaselineReadablePaths(), execPaths(workspaceDir, writableRoots...)) {
		canonicalPath := pathutil.Canonicalise(path)
		if _, isWorkspacePath := pathutil.RelativeTo(workspaceRoot, canonicalPath); isWorkspacePath {
			continue
		}
		if _, wasSeen := seen[canonicalPath]; wasSeen {
			continue
		}
		seen[canonicalPath] = struct{}{}
		paths = append(paths, path)
	}

	return paths
}

func (self Paths) ConfiguredGrants() []PathGrant {
	accessByPath := make(map[string]Access, len(self.Read)+len(self.Write)+len(self.Exec))
	for _, list := range []struct {
		paths  []string
		access Access
	}{
		{self.Read, ReadAccess},
		{self.Write, ReadAccess | WriteAccess},
		{self.Exec, ReadAccess | ExecAccess},
		{self.Path, ReadAccess | ExecAccess},
		{self.Home, ReadAccess},
	} {
		for _, path := range list.paths {
			accessByPath[filepath.Clean(path)] |= list.access
		}
	}

	names := slices.Sorted(maps.Keys(accessByPath))
	grants := make([]PathGrant, 0, len(names))
	for _, path := range names {
		grants = append(grants, PathGrant{Path: path, Access: accessByPath[path]})
	}
	return grants
}

func newMountedRoot(mode *caps.Mode, mount configuredMount, access Access) *file.Root {
	refuseWrite := func(string) error { return file.ErrReadOnly }
	if access.Has(WriteAccess) {
		currentRefusal := caps.RefuseGitWrite(mode)
		refuseWrite = func(name string) error {
			if mount.isExact {
				return currentRefusal(mount.target)
			}
			return currentRefusal(filepath.Join(mount.target, name))
		}
	}
	return file.NewLazy(mount.directory, refuseWrite)
}

func findConfiguredMount(path string) (configuredMount, error) {
	info, err := os.Stat(path)
	if err != nil {
		return configuredMount{}, err
	}

	if info.IsDir() {
		return configuredMount{directory: path, target: path, name: "."}, nil
	}

	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return configuredMount{}, err
	}
	return configuredMount{
		directory: filepath.Dir(target),
		target:    target,
		name:      filepath.Base(target),
		isExact:   true,
	}, nil
}

func probeOpening(path string) error {
	handle, err := os.Open(path) //nolint:gosec // a path the sandbox policy names
	if err != nil {
		return err
	}

	return handle.Close()
}

func closeRoots(roots []*file.Root) {
	for _, root := range roots {
		_ = root.Close()
	}
}
