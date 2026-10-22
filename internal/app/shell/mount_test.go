package shell

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
)

func configuredPathTestRoot(t *testing.T, mode *caps.Mode) *file.Root {
	t.Helper()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return file.New(root, caps.RefuseWrite(mode))
}

func outsideTmpTestDirectory(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(filepath.Join(home, ".cache"), "oh-path-alias-") //nolint:usetesting // the test needs a path outside /tmp
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func hostTmpTestDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(sandbox.TmpDir, "oh-path-target-") //nolint:usetesting // the test needs the host's /tmp
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func TestConfiguredAliasesCrossingTheTmpBoundaryAreRefused(t *testing.T) {
	outside := outsideTmpTestDirectory(t)
	hostTemporary := hostTmpTestDirectory(t)
	intoTmp := filepath.Join(outside, "into-tmp")
	if err := os.Symlink(hostTemporary, intoTmp); err != nil {
		t.Fatal(err)
	}
	outOfTmp := filepath.Join(hostTemporary, "out-of-tmp")
	if err := os.Symlink(outside, outOfTmp); err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{intoTmp, outOfTmp} {
		for name, paths := range map[string]Paths{
			"read":  {Read: []string{alias}},
			"write": {Write: []string{alias}},
			"exec":  {Exec: []string{alias}},
			"path":  {Path: []string{alias}},
			"home":  {Home: []string{alias}},
		} {
			t.Run(name+"/"+filepath.Base(alias), func(t *testing.T) {
				if _, err := PreparePaths(paths, io.Discard); err == nil || !strings.Contains(err.Error(), "/tmp boundary") {
					t.Errorf("configured alias %s was not refused: %v", alias, err)
				}
			})
		}
	}

	missing := filepath.Join(intoTmp, "not-created")
	if _, err := PreparePaths(Paths{Write: []string{missing}}, io.Discard); err == nil {
		t.Error("a missing path under an alias into host tmp was accepted")
	}
	if _, err := os.Stat(filepath.Join(hostTemporary, "not-created")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("preparing the alias created a host tmp path: %v", err)
	}
	if _, err := PreparePaths(Paths{Read: []string{hostTemporary}}, io.Discard); err == nil {
		t.Error("a direct host tmp path was accepted despite the scratch overlay")
	}
}

func TestTemporaryGrantsCannotCrossTheTmpBoundaryThroughAliases(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	outside := outsideTmpTestDirectory(t)
	hostTemporary := hostTmpTestDirectory(t)
	for alias, target := range map[string]string{
		filepath.Join(outside, "into-tmp"):         hostTemporary,
		filepath.Join(hostTemporary, "out-of-tmp"): outside,
	} {
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
		if changed, err := access.Grant(alias, ReadAccess|WriteAccess); changed || err == nil {
			t.Errorf("alias grant changed=%t, err=%v, want refusal", changed, err)
		}
		if slices.Contains(access.GetPaths().Write, alias) {
			t.Errorf("the refused alias %s became writable to the shell", alias)
		}
	}
}

func TestUnconfinedConfiguredPathsCanReachHostTmp(t *testing.T) {
	outside := outsideTmpTestDirectory(t)
	hostTemporary := hostTmpTestDirectory(t)
	alias := filepath.Join(outside, "host-tmp")
	if err := os.Symlink(hostTemporary, alias); err != nil {
		t.Fatal(err)
	}
	prepared, err := PreparePathsForMode(Paths{Read: []string{alias}, Home: []string{alias}}, io.Discard, true)
	if err != nil {
		t.Fatalf("unconfined paths refused host tmp: %v", err)
	}
	if !slices.Contains(prepared.Read, hostTemporary) || !slices.Contains(prepared.Home, alias) {
		t.Errorf("prepared paths did not preserve target and spelling: %+v", prepared)
	}
	mode := caps.NewMode(caps.All())
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccessForMode(files, mode, prepared, true)
	if err != nil {
		t.Errorf("unconfined mount refused the alias: %v", err)
	} else {
		defer access.Close()
		if !access.IsUnconfined() {
			t.Error("host tmp mount forgot that it was unconfined")
		}
	}
}

func mountTestScratch(t *testing.T, files *file.Root, scratch string, isUnconfined bool) []*file.Root {
	t.Helper()

	roots := MountScratch(files, scratch, file.NewLazy(scratch, func(string) error { return nil }), isUnconfined)
	t.Cleanup(func() {
		for _, root := range roots {
			_ = root.Close()
		}
	})
	return roots
}

func TestHostTmpPathsResolveOnlyToSessionScratchWithoutGrants(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	files := file.New(openedRoot, func(string) error { return file.ErrReadOnly })
	scratch := outsideTmpTestDirectory(t)
	mountTestScratch(t, files, scratch, false)
	hostTemporary := hostTmpTestDirectory(t)
	hostFile := filepath.Join(hostTemporary, "note")
	if err := os.WriteFile(hostFile, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	scratchFile := filepath.Join(scratch, filepath.Base(hostTemporary), "note")
	if err := os.MkdirAll(filepath.Dir(scratchFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratchFile, []byte("scratch"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolvedRoot, name, err := files.Resolve(hostFile)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := resolvedRoot.ReadFile(name); err != nil || string(content) != "scratch" {
		t.Errorf("host /tmp spelled path read %q and %v, want only session scratch", content, err)
	}
}

func TestAnUnlistedAliasCannotBorrowTheScratchMount(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	files := file.New(openedRoot, func(string) error { return file.ErrReadOnly })
	scratch := outsideTmpTestDirectory(t)
	mountTestScratch(t, files, scratch, false)
	hostTemporary := hostTmpTestDirectory(t)
	scratchFile := filepath.Join(scratch, filepath.Base(hostTemporary), "note")
	if err := os.MkdirAll(filepath.Dir(scratchFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratchFile, []byte("scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlistedAlias := filepath.Join(outsideTmpTestDirectory(t), "unlisted")
	if err := os.Symlink(hostTemporary, unlistedAlias); err != nil {
		t.Fatal(err)
	}
	if _, _, err := files.Resolve(filepath.Join(unlistedAlias, "note")); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("unlisted alias into host tmp reached scratch: %v", err)
	}
	workspaceAlias := filepath.Join(workspace, "linked-tmp")
	if err := os.Symlink(hostTemporary, workspaceAlias); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, name, err := files.Resolve(filepath.Join(workspaceAlias, "note"))
	if err != nil {
		t.Fatal(err)
	}
	if content, err := resolvedRoot.ReadFile(name); err != nil || string(content) != "scratch" {
		t.Errorf("workspace alias into tmp read %q and %v, want scratch", content, err)
	}
}

func TestMissingConfiguredPathsAreCreatedAndKept(t *testing.T) {
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))
	existingRead := filepath.Join(t.TempDir(), "read")
	if err := os.WriteFile(existingRead, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingWrite := t.TempDir()
	existingExec := filepath.Join(t.TempDir(), "exec")
	if err := os.WriteFile(existingExec, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingHome := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(existingHome, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingRead := filepath.Join(t.TempDir(), "missing-read")
	missingWrite := filepath.Join(t.TempDir(), "missing-write")
	missingExec := filepath.Join(t.TempDir(), "missing-exec")
	missingHome := filepath.Join(t.TempDir(), "missing-home")

	var warnings strings.Builder
	filtered, err := PreparePaths(Paths{
		Read:  []string{existingRead, missingRead},
		Write: []string{existingWrite, missingWrite},
		Exec:  []string{existingExec, missingExec},
		Home:  []string{existingHome, missingHome},
	}, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(filtered.Read, []string{existingRead, missingRead}) {
		t.Errorf("got read paths %v, want %v", filtered.Read, []string{existingRead, missingRead})
	}
	if !slices.Equal(filtered.Write, []string{existingWrite, missingWrite}) {
		t.Errorf("got write paths %v, want %v", filtered.Write, []string{existingWrite, missingWrite})
	}
	if !slices.Equal(filtered.Exec, []string{existingExec, missingExec}) {
		t.Errorf("got executable paths %v, want %v", filtered.Exec, []string{existingExec, missingExec})
	}
	if !slices.Equal(filtered.Home, []string{existingHome, missingHome}) {
		t.Errorf("got mapped paths %v, want %v", filtered.Home, []string{existingHome, missingHome})
	}
	for _, path := range []string{missingRead, missingWrite, missingExec, missingHome} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing path %s was not created: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("missing path %s was not created as a directory", path)
		}
	}
	if warnings.String() != "" {
		t.Errorf("got warnings %q, want none", warnings.String())
	}
}

func TestAReadPathAlreadyCoveredByAnotherListIsWarnedAbout(t *testing.T) {
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))
	written := t.TempDir()
	executed := t.TempDir()
	both := t.TempDir()
	readOnly := t.TempDir()

	var warnings strings.Builder
	filtered, err := PreparePaths(Paths{
		Read:  []string{readOnly, written, executed, both},
		Write: []string{written, both},
		Exec:  []string{executed, both},
	}, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(filtered.Read, []string{readOnly, written, executed, both}) {
		t.Errorf("got read paths %v, want every configured path kept", filtered.Read)
	}

	reported := warnings.String()
	for _, want := range []string{
		"configured path " + pathutil.Shorten(written) + " is redundant in grants.read due to grants.write",
		"configured path " + pathutil.Shorten(executed) + " is redundant in grants.read due to grants.exec",
		"configured path " + pathutil.Shorten(both) + " is redundant in grants.read due to grants.write",
		"configured path " + pathutil.Shorten(both) + " is redundant in grants.read due to grants.exec",
	} {
		if !strings.Contains(reported, want) {
			t.Errorf("warnings do not report %q: %q", want, reported)
		}
	}
	if strings.Contains(reported, pathutil.Shorten(readOnly)+" is redundant") {
		t.Errorf("a read-only path was reported as covered: %q", reported)
	}
}

func TestAPathInBothWriteAndExecIsNotWarnedAbout(t *testing.T) {
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))
	shared := t.TempDir()

	var warnings strings.Builder
	if _, err := PreparePaths(Paths{
		Write: []string{shared},
		Exec:  []string{shared},
	}, &warnings); err != nil {
		t.Fatal(err)
	}
	if warnings.String() != "" {
		t.Errorf("got warnings %q, want none because writing does not grant execution", warnings.String())
	}
}

func TestConfiguredGrantsCombineAccessForEachPath(t *testing.T) {
	paths := Paths{
		Read:  []string{"/read", "/combined"},
		Write: []string{"/write", "/combined"},
		Exec:  []string{"/exec", "/combined"},
		Path:  []string{"/path"},
		Home:  []string{"/home"},
	}
	want := []PathGrant{
		{Path: "/combined", Access: ReadAccess | ExecAccess | WriteAccess},
		{Path: "/exec", Access: ReadAccess | ExecAccess},
		{Path: "/home", Access: ReadAccess},
		{Path: "/path", Access: ReadAccess | ExecAccess},
		{Path: "/read", Access: ReadAccess},
		{Path: "/write", Access: ReadAccess | WriteAccess},
	}
	if got := paths.ConfiguredGrants(); !slices.Equal(got, want) {
		t.Errorf("got grants %#v, want %#v", got, want)
	}
}

func TestConfiguredPathsAreMountedWithTheirRequestedFileAccess(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	mode := caps.NewMode(caps.Read)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	readDirectory := t.TempDir()
	writeDirectory := t.TempDir()
	execDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(writeDirectory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readDirectory, "reference"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	access, err := NewPathAccess(files, mode, Paths{
		Read:  []string{readDirectory},
		Write: []string{writeDirectory},
		Exec:  []string{execDirectory},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	readRoot, name, err := files.Resolve(filepath.Join(readDirectory, "reference"))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := readRoot.ReadFile(name); err != nil || string(data) != "hello" {
		t.Errorf("read got %q and %v", data, err)
	}
	if err := readRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("read path write got %v, want read-only", err)
	}

	writeRoot, name, err := files.Resolve(filepath.Join(writeDirectory, "output"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write path without write capability: %v", err)
	}
	if err := writeRoot.WriteFile(filepath.Join(".git", "config"), nil, 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("repository metadata write got %v, want git refusal", err)
	}
	mode.Toggle(caps.Git)
	if err := writeRoot.WriteFile(filepath.Join(".git", "config"), nil, 0o600); err != nil {
		t.Errorf("repository metadata write with git capability: %v", err)
	}

	execRoot, name, err := files.Resolve(filepath.Join(execDirectory, "proof"))
	if err != nil {
		t.Fatalf("exec path did not resolve through file tools: %v", err)
	}
	if err := execRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("exec path write got %v, want read-only", err)
	}
}

func TestAWriteGrantKeepsItsAccessThroughASymlinkedSpelling(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))

	granted := t.TempDir()
	readDirectory := t.TempDir()
	if err := os.Symlink(granted, filepath.Join(readDirectory, "linked")); err != nil {
		t.Fatal(err)
	}

	access, err := NewPathAccess(files, mode, Paths{
		Read:  []string{readDirectory},
		Write: []string{granted},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	for name, path := range map[string]string{
		"the granted spelling":   filepath.Join(granted, "note.md"),
		"the symlinked spelling": filepath.Join(readDirectory, "linked", "note.md"),
	} {
		t.Run(name, func(t *testing.T) {
			root, resolvedName, err := files.Resolve(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := root.WriteFile(resolvedName, []byte("written"), 0o600); err != nil {
				t.Errorf("%s refused a write the sandbox would allow: %v", name, err)
			}
		})
	}
}

func TestTemporaryAccessOverridesAndThenRestoresConfiguredAccess(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Write)
	files := configuredPathTestRoot(t, mode)
	configuredDirectory := t.TempDir()

	access, err := NewPathAccess(files, mode, Paths{Read: []string{configuredDirectory}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	mountedRoot, name, err := files.Resolve(filepath.Join(configuredDirectory, "proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("configured read path write got %v", err)
	}

	if hasChanged, err := access.Grant(configuredDirectory, ReadAccess|WriteAccess); err != nil || !hasChanged {
		t.Fatalf("grant changed=%t: %v", hasChanged, err)
	}
	_, temporaryPaths := access.getPaths()
	if slices.Contains(temporaryPaths, configuredDirectory) {
		t.Errorf("configured path %s became optional", configuredDirectory)
	}
	mountedRoot, name, err = files.Resolve(filepath.Join(configuredDirectory, "proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !access.Revoke(configuredDirectory) {
		t.Fatal("temporary access was not revoked")
	}
	mountedRoot, name, err = files.Resolve(filepath.Join(configuredDirectory, "proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("blocked again"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("restored read path write got %v", err)
	}
}

func TestRevokingANewTemporaryPathRemovesItFromBothEnforcers(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	temporaryDirectory := t.TempDir()
	if hasChanged, err := access.Grant(temporaryDirectory, ReadAccess); err != nil || !hasChanged {
		t.Fatalf("grant changed=%t: %v", hasChanged, err)
	}
	if _, _, err := files.Resolve(temporaryDirectory); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(access.GetPaths().Read, temporaryDirectory) {
		t.Errorf("shell paths do not include %s", temporaryDirectory)
	}
	_, temporaryPaths := access.getPaths()
	if !slices.Contains(temporaryPaths, temporaryDirectory) {
		t.Errorf("temporary shell paths do not include %s", temporaryDirectory)
	}

	if !access.Revoke(temporaryDirectory) {
		t.Fatal("temporary access was not revoked")
	}
	if _, _, err := files.Resolve(temporaryDirectory); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("revoked file path resolved with %v", err)
	}
	if slices.Contains(access.GetPaths().Read, temporaryDirectory) {
		t.Errorf("shell paths kept %s", temporaryDirectory)
	}
}

func TestAReadGrantIsExecutableByTheShell(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	temporaryDirectory := t.TempDir()
	if hasChanged, err := access.Grant(temporaryDirectory, ReadAccess); err != nil || !hasChanged {
		t.Fatalf("grant changed=%t: %v", hasChanged, err)
	}
	if _, _, err := files.Resolve(temporaryDirectory); err != nil {
		t.Errorf("temporary path did not resolve through file tools: %v", err)
	}
	paths := access.GetPaths()
	if !slices.Contains(paths.Exec, temporaryDirectory) {
		t.Errorf("shell paths do not execute %s", temporaryDirectory)
	}
	if !slices.Contains(paths.Read, temporaryDirectory) {
		t.Errorf("shell paths do not read %s", temporaryDirectory)
	}

	if !access.Revoke(temporaryDirectory) {
		t.Fatal("temporary access was not revoked")
	}
	if slices.Contains(access.GetPaths().Exec, temporaryDirectory) {
		t.Errorf("shell paths kept %s", temporaryDirectory)
	}
}

func TestATemporaryGrantNeedsAPathThatExists(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if _, err := access.Grant(filepath.Join(t.TempDir(), "absent"), ReadAccess); err == nil {
		t.Error("expected an absent path not to be granted")
	}
}

func TestTemporaryMountsCanChangeWhileFileToolsResolvePaths(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	temporaryDirectory := t.TempDir()
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		for range 100 {
			_, _ = access.Grant(temporaryDirectory, ReadAccess)
			access.Revoke(temporaryDirectory)
		}
	}()
	go func() {
		defer waitGroup.Done()
		for range 1000 {
			_, _, _ = files.Resolve(temporaryDirectory)
		}
	}()
	waitGroup.Wait()
}

func TestConfiguredFilesAreMountedWithoutTheirSiblings(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	directory := t.TempDir()
	readPath := filepath.Join(directory, "reference")
	writePath := filepath.Join(directory, "output")
	siblingPath := filepath.Join(directory, "private")
	if err := os.WriteFile(readPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writePath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	access, err := NewPathAccess(files, mode, Paths{
		Read:  []string{readPath, writePath},
		Write: []string{writePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	readRoot, name, err := files.Resolve(readPath)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := readRoot.ReadFile(name); err != nil || string(data) != "hello" {
		t.Errorf("read got %q and %v", data, err)
	}
	if err := readRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("read file write got %v, want read-only", err)
	}

	writeRoot, name, err := files.Resolve(writePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write file without write capability: %v", err)
	}

	if _, _, err := files.Resolve(siblingPath); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("sibling resolved through file tools with %v", err)
	}
}

func TestAConfiguredFileSymlinkCannotDisguiseRepositoryMetadata(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Write)
	files := configuredPathTestRoot(t, mode)
	repository := outsideTmpTestDirectory(t)
	target := filepath.Join(repository, ".git", "config")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(outsideTmpTestDirectory(t), "shared")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}

	access, err := NewPathAccess(files, mode, Paths{Write: []string{alias}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	mountedRoot, name, err := files.Resolve(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("repository metadata write got %v, want git refusal", err)
	}
	mode.Toggle(caps.Git)
	if err := mountedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Errorf("repository metadata write with git capability: %v", err)
	}
}

func spelledThroughASymlink(t *testing.T) (string, string) {
	t.Helper()

	system, err := filepath.EvalSymlinks(outsideTmpTestDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(system, "oh", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}

	elsewhere := outsideTmpTestDirectory(t)
	if err := os.Symlink(system, filepath.Join(elsewhere, "org.crdx")); err != nil {
		t.Fatal(err)
	}

	return system, filepath.Join(elsewhere, "org.crdx")
}

func TestAGrantIsHeldByItsRealPathWhileAHomeMappingKeepsItsSpelling(t *testing.T) {
	system, spelled := spelledThroughASymlink(t)
	homeMapping := filepath.Join(spelled, "oh", "skills")

	prepared, err := PreparePaths(Paths{
		Read:  []string{filepath.Join(spelled, "oh")},
		Write: []string{filepath.Join(spelled, "oh", "skills")},
		Home:  []string{homeMapping},
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(system, "oh"); prepared.Read[0] != want {
		t.Errorf("read grant is %q, want the real path %q", prepared.Read[0], want)
	}
	if want := filepath.Join(system, "oh", "skills"); prepared.Write[0] != want {
		t.Errorf("write grant is %q, want the real path %q", prepared.Write[0], want)
	}
	if prepared.Home[0] != homeMapping {
		t.Errorf("home mapping is %q, want the spelling it was given", prepared.Home[0])
	}
}

func TestAGrantSwallowedByAWiderGrantIsReported(t *testing.T) {
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		paths Paths
		want  string
	}{
		"a read grant inside a writable path": {
			paths: Paths{Read: []string{inner}, Write: []string{outer}},
			want:  "is writable but holds the read-only",
		},
		"a read grant inside an executable path": {
			paths: Paths{Read: []string{inner}, Exec: []string{outer}},
			want:  "is executable but holds the read-only",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var warnings strings.Builder
			if _, err := PreparePaths(test.paths, &warnings); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(warnings.String(), test.want) {
				t.Errorf("got %q, want it to say %q", warnings.String(), test.want)
			}
		})
	}
}

func TestAGrantRefiningAReadGrantIsNotReported(t *testing.T) {
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, paths := range map[string]Paths{
		"a writable path inside a read grant":    {Read: []string{outer}, Write: []string{inner}},
		"an executable path inside a read grant": {Read: []string{outer}, Exec: []string{inner}},
	} {
		t.Run(name, func(t *testing.T) {
			var warnings strings.Builder
			if _, err := PreparePaths(paths, &warnings); err != nil {
				t.Fatal(err)
			}
			if warnings.String() != "" {
				t.Errorf("got %q, want nothing said about a grant refining a read grant", warnings.String())
			}
		})
	}
}

func TestNestedGrantsOnTheirOwnAreNotReported(t *testing.T) {
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}

	var warnings strings.Builder
	if _, err := PreparePaths(Paths{Read: []string{outer, inner}}, &warnings); err != nil {
		t.Fatal(err)
	}
	if warnings.String() != "" {
		t.Errorf("got %q, want nothing said about two paths of the same kind", warnings.String())
	}
}

func TestAWriteGrantIsReachedThroughTheSpellingTheConfigurationUsed(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	_, spelled := spelledThroughASymlink(t)
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))

	prepared, err := PreparePaths(Paths{
		Read:  []string{filepath.Join(spelled, "oh")},
		Write: []string{filepath.Join(spelled, "oh", "skills")},
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	access, err := NewPathAccess(files, mode, prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	root, name, err := files.Resolve(filepath.Join(spelled, "oh", "skills", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(name, []byte("edited"), 0o600); err != nil {
		t.Errorf("the configured spelling refused a write: %v", err)
	}
}

func TestBaselineSystemPathsAreMountedReadOnlyForFileTools(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Write)
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))

	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	for name, path := range map[string]string{
		"direct path": "/usr",
		"workspace symlink": func() string {
			alias := filepath.Join(workspace, "system")
			if err := os.Symlink("/usr", alias); err != nil {
				t.Fatal(err)
			}
			return alias
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			mountedRoot, resolvedName, err := files.Resolve(path)
			if err != nil {
				t.Fatalf("baseline path did not resolve through file tools: %v", err)
			}
			if _, err := mountedRoot.Stat(resolvedName); err != nil {
				t.Fatalf("baseline path was not readable: %v", err)
			}
			if err := mountedRoot.RefuseWrite(resolvedName); !errors.Is(err, file.ErrReadOnly) {
				t.Errorf("baseline path write got %v, want read-only", err)
			}
		})
	}
	aliasFile := filepath.Join(workspace, "system", "lib", "os-release")
	mountedRoot, resolvedName, err := files.Resolve(aliasFile)
	if err != nil {
		t.Fatalf("file beneath workspace symlink did not resolve through its baseline mount: %v", err)
	}
	if data, err := mountedRoot.ReadFile(resolvedName); err != nil || len(data) == 0 {
		t.Errorf("file beneath workspace symlink read %d bytes and %v", len(data), err)
	}

	for _, path := range []string{"/etc/passwd", "/etc/machine-id"} {
		exactRoot, exactName, err := files.Resolve(path)
		if err != nil {
			t.Fatalf("exact baseline file %s did not resolve: %v", path, err)
		}
		if data, err := exactRoot.ReadFile(exactName); err != nil || len(data) == 0 {
			t.Errorf("exact baseline file %s read %d bytes and %v", path, len(data), err)
		}
	}

	for _, path := range sandbox.BaselineReadablePaths() {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			t.Errorf("could not inspect existing baseline path %s: %v", path, err)
			continue
		}

		mountedRoot, resolvedName, err := files.Resolve(path)
		if err != nil {
			t.Errorf("baseline path %s did not resolve through file tools: %v", path, err)
			continue
		}
		if _, err := mountedRoot.Stat(resolvedName); err != nil {
			t.Errorf("baseline path %s was not readable: %v", path, err)
		}
		if err := mountedRoot.RefuseWrite(resolvedName); !errors.Is(err, file.ErrReadOnly) {
			t.Errorf("baseline path %s write got %v, want read-only", path, err)
		}
	}

	if slices.Contains(access.GetPaths().Read, "/usr") {
		t.Error("implicit baseline path was reported as an explicit sandbox path")
	}
}

func TestRevokingTemporaryAccessRestoresBaselineSystemAccess(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Write)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if changed, err := access.Grant("/usr", ReadAccess|WriteAccess|ExecAccess); err != nil || !changed {
		t.Fatalf("grant changed=%t: %v", changed, err)
	}
	mountedRoot, name, err := files.Resolve("/usr")
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.RefuseWrite(name); err != nil {
		t.Errorf("temporary write access remained read-only: %v", err)
	}

	if !access.Revoke("/usr") {
		t.Fatal("temporary access was not revoked")
	}
	mountedRoot, name, err = files.Resolve("/usr")
	if err != nil {
		t.Fatalf("revoking temporary access removed baseline access: %v", err)
	}
	if err := mountedRoot.RefuseWrite(name); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("restored baseline write got %v, want read-only", err)
	}
}

func TestInheritedPathDirectoriesAreReadableByFileToolsAndExecutableByShells(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	executableDirectory := t.TempDir()
	proof := filepath.Join(executableDirectory, "proof")
	if err := os.WriteFile(proof, []byte("executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", executableDirectory)

	mode := caps.NewMode(caps.Read | caps.Shell)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	mountedRoot, name, err := files.Resolve(proof)
	if err != nil {
		t.Fatalf("PATH file did not resolve through file tools: %v", err)
	}
	if data, err := mountedRoot.ReadFile(name); err != nil || string(data) != "executable" {
		t.Errorf("PATH file read got %q and %v", data, err)
	}
	if err := mountedRoot.RefuseWrite(name); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("PATH file write got %v, want read-only", err)
	}

	policy, err := createTestPolicy(t, workspace, t.TempDir(), t.TempDir(), Paths{}, mode.Current())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(policy.Exec, executableDirectory) {
		t.Errorf("shell executable paths %v do not include PATH directory %s", policy.Exec, executableDirectory)
	}
}

func TestHomeMappingsAreReadableByFileToolsBeforeAShellRuns(t *testing.T) {
	for name, isLinked := range map[string]bool{"a file": false, "a link to a file": true} {
		t.Run(name, func(t *testing.T) {
			relative := filepath.Join(".config", "git", "ignore")
			var source string
			if isLinked {
				linkedHome, targetDirectory := outsideTmpTestDirectory(t), outsideTmpTestDirectory(t)
				target := filepath.Join(targetDirectory, "ignore")
				if err := os.WriteFile(target, []byte("*.tmp\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", linkedHome)
				source = filepath.Join(linkedHome, relative)
				if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, source); err != nil {
					t.Fatal(err)
				}
			} else {
				source = userHomeFile(t, relative, "*.tmp\n")
			}
			workspace := t.TempDir()
			workspaceRoot, err := os.OpenRoot(workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspaceRoot.Close() }()

			privateHome := t.TempDir()
			tmp := t.TempDir()
			paths := Paths{Home: []string{source}}
			mode := caps.NewMode(caps.Read)
			if err := PrepareHomeMappings(workspace, testPlaces(t, privateHome, tmp), paths, mode.Current()); err != nil {
				t.Fatal(err)
			}

			files := file.New(workspaceRoot, caps.RefuseWrite(mode))
			homeRoot := file.NewLazy(privateHome, RefuseHomeWrite(mode))
			files.Mount(privateHome, homeRoot)
			defer func() { _ = homeRoot.Close() }()
			access, err := NewPathAccess(files, mode, paths)
			if err != nil {
				t.Fatal(err)
			}
			defer access.Close()
			access.MountHomeMappings(privateHome)

			mapped := filepath.Join(privateHome, ".config", "git", "ignore")
			mountedRoot, name, err := files.Resolve(mapped)
			if err != nil {
				t.Fatalf("home mapping did not resolve through file tools: %v", err)
			}
			if data, err := mountedRoot.ReadFile(name); err != nil || string(data) != "*.tmp\n" {
				t.Errorf("home mapping read got %q and %v", data, err)
			}
			if err := mountedRoot.RefuseWrite(name); !errors.Is(err, file.ErrReadOnly) {
				t.Errorf("home mapping write got %v, want read-only", err)
			}
		})
	}
}

func TestASharedSkillIsReadableAndExecutableByTheShell(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Shell)
	files := configuredPathTestRoot(t, mode)
	configuredDirectory := t.TempDir()
	access, err := NewPathAccess(files, mode, Paths{Exec: []string{configuredDirectory}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	skillDirectory := t.TempDir()
	linkedSkill := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(skillDirectory, linkedSkill); err != nil {
		t.Fatal(err)
	}
	access.ShareSkills([]string{linkedSkill, configuredDirectory})

	paths := access.GetPaths()
	for _, list := range [][]string{paths.Read, paths.Exec} {
		if !slices.Contains(list, skillDirectory) {
			t.Errorf("shell paths %v do not reach the skill's real directory %s", list, skillDirectory)
		}
		if slices.Contains(list, linkedSkill) {
			t.Errorf("shell paths %v name the symbolic link %s", list, linkedSkill)
		}
	}
	if count := countOf(paths.Exec, configuredDirectory); count != 1 {
		t.Errorf("configured directory is executable %d times, want once: %v", count, paths.Exec)
	}

	policy, err := createTestPolicy(t, t.TempDir(), t.TempDir(), t.TempDir(), paths, caps.Shell)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the shared skill here: %v", err)
	}
	if !slices.Contains(policy.Read, skillDirectory) || !slices.Contains(policy.Exec, skillDirectory) {
		t.Errorf("policy does not read and execute %s: read %v, exec %v", skillDirectory, policy.Read, policy.Exec)
	}

	access.ShareSkills(nil)
	if slices.Contains(access.GetPaths().Read, skillDirectory) {
		t.Errorf("shell paths kept the skill after it stopped being shared")
	}
}

func countOf(list []string, wanted string) int {
	count := 0
	for _, item := range list {
		if item == wanted {
			count++
		}
	}
	return count
}

func TestPathAccessHoldsADescriptorOnlyForWhatIsRead(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	var readDirectories []string
	for range 12 {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "reference"), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		readDirectories = append(readDirectories, directory)
	}

	before := openDescriptors(t)
	access, err := NewPathAccess(files, mode, Paths{Read: readDirectories})
	if err != nil {
		t.Fatal(err)
	}
	if held := openDescriptors(t) - before; held != 0 {
		t.Errorf("mounting held %d descriptors, want none", held)
	}

	readRoot, name, err := files.Resolve(filepath.Join(readDirectories[0], "reference"))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := readRoot.ReadFile(name); err != nil || string(data) != "hello" {
		t.Fatalf("read got %q and %v", data, err)
	}
	if held := openDescriptors(t) - before; held != 1 {
		t.Errorf("one read held %d descriptors, want 1", held)
	}

	if _, err := access.Grant(t.TempDir(), ReadAccess); err != nil {
		t.Fatal(err)
	}
	if held := openDescriptors(t) - before; held != 2 {
		t.Errorf("a temporary grant held %d descriptors beside the read, want 2", held)
	}

	access.Close()
	if held := openDescriptors(t) - before; held != 0 {
		t.Errorf("closing left %d descriptors held, want none", held)
	}
}

func openDescriptors(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}

	return len(entries)
}

func TestGitAloneCannotCreateMetadataOrFollowItsAlias(t *testing.T) {
	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read | caps.Git)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if err := files.MkdirAll(".git/hooks", 0o700); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("creating missing metadata with g but not w: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".git")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("metadata was created: %v", err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "ordinary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ordinary", filepath.Join(workspace, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := files.WriteFile(".git/config", []byte("write"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("writing through metadata alias with g but not w: %v", err)
	}
	if err := os.Remove(filepath.Join(workspace, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := files.WriteFile(".git/config", []byte("allowed"), 0o600); err != nil {
		t.Errorf("writing into existing metadata with g: %v", err)
	}
}

func TestAWorkspaceWriteGrantLetsFileToolsWriteUntilItIsRevoked(t *testing.T) {
	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Deny: []string{"*.env"}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if err := files.WriteFile("note", []byte("before"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write before the grant: %v", err)
	}
	if changed, err := access.Grant(workspace, ReadAccess|WriteAccess); err != nil || !changed {
		t.Fatalf("grant changed=%t: %v", changed, err)
	}
	if err := files.WriteFile("note", []byte("granted"), 0o600); err != nil {
		t.Errorf("relative workspace write: %v", err)
	}
	if err := files.WriteFile(".git/config", []byte("blocked"), 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("write into repository metadata without g: %v", err)
	}
	if err := files.WriteFile("auth.env", []byte("blocked"), 0o600); err == nil {
		t.Error("write grant overrode a denied name")
	}
	if !access.Revoke(workspace) {
		t.Fatal("workspace grant could not be revoked")
	}
	if err := files.WriteFile("note", []byte("after"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write after revocation: %v", err)
	}
}

func TestAWriteGrantInsideTheWorkspaceDoesNotUnlockItsSiblings(t *testing.T) {
	workspace := t.TempDir()
	granted := filepath.Join(workspace, "granted")
	if err := os.Mkdir(granted, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{granted}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if err := files.WriteFile("granted/note", []byte("allowed"), 0o600); err != nil {
		t.Errorf("configured write grant: %v", err)
	}
	if err := files.WriteFile("sibling", []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write beside a narrower grant: %v", err)
	}
}

func TestNestedReadAndExecPathsNarrowWorkspaceFileToolWrites(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	for _, name := range []string{"read/writable", "exec", "path"} {
		if err := os.MkdirAll(filepath.Join(workspace, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("read", filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{
		Read:  []string{filepath.Join(workspace, "read")},
		Exec:  []string{filepath.Join(workspace, "exec")},
		Path:  []string{filepath.Join(workspace, "path")},
		Write: []string{filepath.Join(workspace, "read", "writable")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	for _, name := range []string{"read/file", "alias/file", "exec/file", "path/file"} {
		if err := files.RefuseWrite(name); !errors.Is(err, file.ErrReadOnly) {
			t.Errorf("narrower read or exec path %s got %v, want read-only", name, err)
		}
	}
	for _, name := range []string{"read/writable/file", "sibling"} {
		if err := files.RefuseWrite(name); err != nil {
			t.Errorf("write outside the narrower read mount %s: %v", name, err)
		}
	}
}

func TestTemporaryReadNarrowsAConfiguredWriteUntilRevoked(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	parent := filepath.Join(workspace, "writable")
	child := filepath.Join(parent, "read-only")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{parent}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	if changed, err := access.Grant(child, ReadAccess); err != nil || !changed {
		t.Fatalf("temporary read changed=%t: %v", changed, err)
	}
	if err := files.RefuseWrite("writable/read-only/file"); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("temporary read did not narrow the child: %v", err)
	}
	if err := files.RefuseWrite("writable/file"); err != nil {
		t.Errorf("configured writable parent was narrowed: %v", err)
	}
	if !access.Revoke(child) {
		t.Fatal("temporary read could not be revoked")
	}
	if err := files.RefuseWrite("writable/read-only/file"); err != nil {
		t.Errorf("revoking the read grant did not restore the write: %v", err)
	}
}

func TestATemporaryReadKeepsAConfiguredWriteAtTheSamePath(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	shared := filepath.Join(workspace, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{shared}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	if changed, err := access.Grant(shared, ReadAccess); err != nil || !changed {
		t.Fatalf("temporary read changed=%t: %v", changed, err)
	}
	if err := files.RefuseWrite("shared/file"); err != nil {
		t.Errorf("a read grant narrowed a configured write on the same path: %v", err)
	}
	mountedRoot, name, err := files.Resolve(filepath.Join(shared, "file"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.RefuseWrite(name); err != nil {
		t.Errorf("the mounted root lost its configured write: %v", err)
	}
}

func TestWriteAndHomeAliasOfTheSameDirectoryChooseWriteEveryTime(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	writable := filepath.Join(workspace, "writable")
	if err := os.Mkdir(writable, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "home-alias")
	if err := os.Symlink("writable", alias); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{writable}, Home: []string{alias}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	for range 100 {
		if err := files.RefuseWrite("writable/note"); err != nil {
			t.Fatalf("a read-only alias narrowed the configured write: %v", err)
		}
	}
}

func TestAConfiguredReadMountCannotChangeBeforeItsFirstRead(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	parent := outsideTmpTestDirectory(t)
	granted := filepath.Join(parent, "reference")
	private := filepath.Join(parent, "private")
	for _, directory := range []string{granted, private} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(private, "marker"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	mode := caps.NewMode(caps.Read)
	files := file.New(openedRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Read: []string{granted}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	mountedRoot, name, err := files.Resolve(filepath.Join(granted, "marker"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(granted, granted+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, granted); err != nil {
		t.Fatal(err)
	}
	if content, err := mountedRoot.ReadFile(name); !errors.Is(err, file.ErrMountChanged) || len(content) != 0 {
		t.Errorf("retargeted configured read returned %q and %v", content, err)
	}
}

func TestAConfiguredMountCannotMoveIntoGitBeforeItsFirstWrite(t *testing.T) {
	workspace := outsideTmpTestDirectory(t)
	parent := outsideTmpTestDirectory(t)
	grant := filepath.Join(parent, "grant")
	metadata := filepath.Join(parent, ".git")
	for _, directory := range []string{grant, metadata} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	metadataRoot, err := os.OpenRoot(metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = metadataRoot.Close() }()
	if err := metadataRoot.WriteFile("config", []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(openedRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{grant}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	mountedRoot, name, err := files.Resolve(filepath.Join(grant, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(grant, grant+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(metadata, grant); err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrMountChanged) {
		t.Errorf("retargeted lazy grant got %v, want mount-change refusal", err)
	}
	if _, err := mountedRoot.ReadFile(name); !errors.Is(err, file.ErrMountChanged) {
		t.Errorf("retargeted lazy grant read got %v, want the same refusal", err)
	}
	if content, err := metadataRoot.ReadFile("config"); err != nil || string(content) != "intact" {
		t.Errorf("repository metadata changed to %q: %v", content, err)
	}
}

func TestAFileToolCannotAlterHardlinkedMetadataInEitherMode(t *testing.T) {
	workspace := t.TempDir()
	metadata := filepath.Join(workspace, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(metadata), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(metadata, filepath.Join(workspace, "ordinary")); err != nil {
		t.Fatal(err)
	}
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(openedRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if err := files.WriteFile("ordinary", []byte("changed"), 0o600); !errors.Is(err, file.ErrMultipleHardLinks) {
		t.Errorf("write without g got %v, want hard-link refusal", err)
	}
	if content, err := files.ReadFile(".git/config"); err != nil || string(content) != "intact" {
		t.Errorf("repository metadata changed to %q: %v", content, err)
	}
	mode.Toggle(caps.Git)
	if err := files.WriteFile("ordinary", []byte("changed"), 0o600); !errors.Is(err, file.ErrMultipleHardLinks) {
		t.Errorf("write with g got %v, want the same hard-link refusal", err)
	}
	if content, err := files.ReadFile(".git/config"); err != nil || string(content) != "intact" {
		t.Errorf("g let the shared inode change to %q: %v", content, err)
	}
	if err := os.Remove(filepath.Join(workspace, "ordinary")); err != nil {
		t.Fatal(err)
	}
	if err := files.WriteFile(".git/config", []byte("allowed"), 0o600); err != nil {
		t.Errorf("g did not permit a single-link metadata write: %v", err)
	}
	if content, err := files.ReadFile(".git/config"); err != nil || string(content) != "allowed" {
		t.Errorf("single-link metadata remained %q: %v", content, err)
	}
}

func TestAnExactWriteGrantCannotAlterHardlinkedMetadata(t *testing.T) {
	parent := outsideTmpTestDirectory(t)
	workspace := filepath.Join(parent, "workspace")
	aliasDirectory := filepath.Join(parent, "granted")
	metadata := filepath.Join(workspace, ".git", "config")
	for _, directory := range []string{filepath.Dir(metadata), aliasDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(metadata, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasDirectory, "shared")
	if err := os.Link(metadata, alias); err != nil {
		t.Fatal(err)
	}
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(openedRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	if changed, err := access.Grant(alias, ReadAccess|WriteAccess); err != nil || !changed {
		t.Fatalf("exact grant changed=%t: %v", changed, err)
	}
	mountedRoot, name, err := files.Resolve(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrMultipleHardLinks) {
		t.Errorf("write under an exact grant got %v, want hard-link refusal", err)
	}
	if content, err := files.ReadFile(".git/config"); err != nil || string(content) != "intact" {
		t.Errorf("repository metadata changed to %q: %v", content, err)
	}
	mode.Toggle(caps.Git)
	if err := mountedRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrMultipleHardLinks) {
		t.Errorf("exact grant with g got %v, want hard-link refusal", err)
	}
}

func TestAChangingSymlinkCannotRedirectAFileToolWriteIntoGit(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{".git", "ordinary"} {
		if err := os.Mkdir(filepath.Join(workspace, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	metadata := filepath.Join(workspace, ".git", "config")
	if err := os.WriteFile(metadata, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	alias := filepath.Join(workspace, "alias")
	finished := make(chan struct{})
	var swapping sync.WaitGroup
	swapping.Go(func() {
		for {
			select {
			case <-finished:
				return
			default:
			}
			for _, target := range []string{"ordinary", ".git"} {
				_ = os.Remove(alias)
				_ = os.Symlink(target, alias)
			}
		}
	})
	for range 1000 {
		_ = files.WriteFile("alias/config", []byte("changed"), 0o600)
	}
	close(finished)
	swapping.Wait()
	content, err := os.ReadFile(metadata) //nolint:gosec // a path made by this test
	if err != nil || string(content) != "intact" {
		t.Errorf("repository metadata changed to %q: %v", content, err)
	}
}

func TestTemporaryMountIsWritableBeneathAConfiguredReadPath(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })

	farm := outsideTmpTestDirectory(t)
	scratch := filepath.Join(farm, "tranquil-guppy")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}

	paths, err := PreparePaths(Paths{Read: []string{farm}}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	mode := caps.NewMode(caps.Read | caps.Shell | caps.Subagents)
	access, err := NewPathAccess(files, mode, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	mountTestScratch(t, files, scratch, false)

	resolvedRoot, name, err := files.Resolve("/tmp/probe.txt")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := resolvedRoot.RefuseWrite(name); err != nil {
		t.Fatalf("the scratch was not writable: %v", err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestAConfiguredExactFileCannotBeRedirectedToASiblingByASymlinkSwap(t *testing.T) {
	for name, git := range map[string]bool{"without git": false, "with git": true} {
		t.Run(name, func(t *testing.T) {
			mode := caps.NewMode(caps.Read | caps.Write)
			if git {
				mode.Toggle(caps.Git)
			}
			files := configuredPathTestRoot(t, mode)
			directory := t.TempDir()
			grantedPath := filepath.Join(directory, "granted")
			siblingPath := filepath.Join(directory, "private")
			const canary = "canary-secret-9f3a71"

			if err := os.WriteFile(grantedPath, []byte("granted-content"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(siblingPath, []byte(canary), 0o600); err != nil {
				t.Fatal(err)
			}

			access, err := NewPathAccess(files, mode, Paths{
				Read:  []string{grantedPath},
				Write: []string{grantedPath},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer access.Close()

			if err := os.Remove(grantedPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("private", grantedPath); err != nil {
				t.Fatal(err)
			}

			readRoot, name, err := files.Resolve(grantedPath)
			if err != nil {
				t.Fatal(err)
			}
			if data, readErr := readRoot.ReadFile(name); !errors.Is(readErr, file.ErrOutsideRoot) {
				t.Errorf("read got %q and %v, want an outside-the-root refusal", data, readErr)
			}

			writeRoot, writeName, err := files.Resolve(grantedPath)
			if err != nil {
				t.Fatal(err)
			}
			if writeErr := writeRoot.WriteFile(writeName, []byte("tampered"), 0o600); !errors.Is(writeErr, file.ErrOutsideRoot) {
				t.Errorf("write got %v, want an outside-the-root refusal", writeErr)
			}
			after, err := os.ReadFile(siblingPath) //nolint:gosec // a path this test made itself
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != canary {
				t.Errorf("write changed the sibling to %q", after)
			}
		})
	}
}

func TestAHomeMappedExactFileCannotBeRedirectedToASiblingByASymlinkSwap(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	realHome := t.TempDir()
	t.Setenv("HOME", realHome)
	relative := filepath.Join(".config", "secrets", "token")
	source := filepath.Join(realHome, relative)
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("granted-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(source), "private")
	const canary = "canary-secret-77c2b0"
	if err := os.WriteFile(sibling, []byte(canary), 0o600); err != nil {
		t.Fatal(err)
	}

	privateHome := t.TempDir()
	paths := Paths{Home: []string{source}}
	if err := PrepareHomeMappings(workspace, testPlaces(t, privateHome, t.TempDir()), paths, mode.Current()); err != nil {
		t.Fatal(err)
	}

	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	homeRoot := file.NewLazy(privateHome, RefuseHomeWrite(mode))
	files.Mount(privateHome, homeRoot)
	defer func() { _ = homeRoot.Close() }()

	access, err := NewPathAccess(files, mode, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	access.MountHomeMappings(privateHome)

	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("private", source); err != nil {
		t.Fatal(err)
	}

	mapped := filepath.Join(privateHome, relative)
	mountedRoot, name, err := files.Resolve(mapped)
	if err != nil {
		t.Fatal(err)
	}
	if data, readErr := mountedRoot.ReadFile(name); readErr == nil {
		t.Errorf("home-mapped read followed the swapped symlink and returned %q, want a refusal", data)
	}
}
