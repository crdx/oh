package shell

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
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

func TestMissingConfiguredPathsAreCreatedAndKept(t *testing.T) {
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
		"configured path " + written + " is redundant in [sandbox.read] due to sandbox.write",
		"configured path " + executed + " is redundant in [sandbox.read] due to sandbox.exec",
		"configured path " + both + " is redundant in [sandbox.read] due to sandbox.write",
		"configured path " + both + " is redundant in [sandbox.read] due to sandbox.exec",
	} {
		if !strings.Contains(reported, want) {
			t.Errorf("warnings do not report %q: %q", want, reported)
		}
	}
	if strings.Contains(reported, readOnly+" is redundant") {
		t.Errorf("a read-only path was reported as covered: %q", reported)
	}
}

func TestAPathInBothWriteAndExecIsNotWarnedAbout(t *testing.T) {
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
	if err := writeRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write path without write capability got %v, want read-only", err)
	}
	mode.Toggle(caps.Write)
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write path with write capability: %v", err)
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

func TestAnExecutableGrantIsReadableAndReplacesTheMountBeneathIt(t *testing.T) {
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

	if hasChanged, err := access.Grant(temporaryDirectory, ReadAccess|ExecAccess); err != nil || !hasChanged {
		t.Fatalf("executable grant changed=%t: %v", hasChanged, err)
	}
	if _, _, err := files.Resolve(temporaryDirectory); err != nil {
		t.Errorf("executable path did not resolve through file tools: %v", err)
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

func TestAnExecutableGrantNeedsAPathThatExists(t *testing.T) {
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if _, err := access.Grant(filepath.Join(t.TempDir(), "absent"), ReadAccess|ExecAccess); err == nil {
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
	if err := writeRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write file without write capability got %v, want read-only", err)
	}
	mode.Toggle(caps.Write)
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write file with write capability: %v", err)
	}

	if _, _, err := files.Resolve(siblingPath); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("sibling resolved through file tools with %v", err)
	}
}

func TestAConfiguredFileSymlinkCannotDisguiseRepositoryMetadata(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Write)
	files := configuredPathTestRoot(t, mode)
	repository := t.TempDir()
	target := filepath.Join(repository, ".git", "config")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "shared")
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

	system, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(system, "oh", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}

	elsewhere := t.TempDir()
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
	source := userHomeFile(t, filepath.Join(".config", "git", "ignore"), "*.tmp\n")
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
	if err := PrepareHomeMappings(workspace, privateHome, tmp, paths, mode.Current()); err != nil {
		t.Fatal(err)
	}

	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	homeRoot, err := MountHomeDirectory(files, privateHome, mode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = homeRoot.Close() }()
	access, err := NewPathAccess(files, mode, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

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
}
