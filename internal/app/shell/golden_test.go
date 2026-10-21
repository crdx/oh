package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
)

func TestGoldenShellPoliciesHandleFilesystemChanges(t *testing.T) {
	sections := []string{
		"=== workspace PATH aliases ===\n" + workspacePathAliasesGolden(t),
		"=== PATH alias outside workspace ===\n" + outsidePathAliasGolden(t),
		"=== unreadable workspace subtree mode 000 ===\n" + unreadableSubtreeGolden(t, 0, false),
		"=== unreadable additional subtree mode 000 ===\n" + unreadableSubtreeGolden(t, 0, true),
		"=== unreadable workspace subtree mode 0300 ===\n" + unreadableSubtreeGolden(t, 0o300, false),
		"=== unreadable additional subtree mode 0300 ===\n" + unreadableSubtreeGolden(t, 0o300, true),
		"=== live read grant ===\n" + temporaryGrantGolden(t, ReadAccess, true),
		"=== live read and write grant ===\n" + temporaryGrantGolden(t, ReadAccess|WriteAccess, true),
		"=== vanished read grant ===\n" + temporaryGrantGolden(t, ReadAccess, false),
		"=== vanished read and write grant ===\n" + temporaryGrantGolden(t, ReadAccess|WriteAccess, false),
		"=== configured path with temporary write access ===\n" + configuredPathGolden(t),
		"=== missing protection root ===\n" + missingProtectionRootGolden(t),
		"=== symlinked workspace metadata ===\n" + symlinkedWorkspaceGolden(t),
		"=== git-only file tools ===\n" + gitOnlyGolden(t),
		"=== multiply-linked metadata ===\n" + hardLinkGolden(t),
		"=== explicit workspace write grant ===\n" + workspaceWriteGrantGolden(t),
		"=== nested write grant scope ===\n" + nestedWriteGrantGolden(t),
		"=== aliased write grant scope ===\n" + aliasedWriteGrantGolden(t),
		"=== configured mount changed before use ===\n" + changedMountedPathGolden(t),
		"=== git withdrawal job scope ===\n" + gitWithdrawalGolden(),
		"=== absolute denied home secret ===\n" + absoluteDenyGolden(t),
		"=== host tmp aliases refused ===\n" + hostTmpAliasGolden(t),
		"=== virtual tmp mount attribution ===\n" + virtualTmpMountGolden(t),
		"=== unconfined host tmp ===\n" + yoloTmpGolden(t),
	}
	compareShellGolden(t, "policies.txt", strings.Join(sections, "\n\n")+"\n")
}

func workspacePathAliasesGolden(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()
	versionDirectory := filepath.Join(workspace, "tools", "1.0")
	if err := os.MkdirAll(versionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "tools", "latest")
	if err := os.Symlink("1.0", alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{versionDirectory, alias, filepath.Join("tools", "latest")}, string(os.PathListSeparator)))

	return renderPaths(workspace, execPaths(workspace))
}

func outsidePathAliasGolden(t *testing.T) string {
	t.Helper()

	workspace := outsideTmpTestDirectory(t)
	outside := outsideTmpTestDirectory(t)
	alias := filepath.Join(workspace, "latest")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", alias)

	return renderPaths(workspace, execPaths(workspace))
}

func unreadableSubtreeGolden(t *testing.T, mode os.FileMode, isAdditional bool) string {
	t.Helper()

	workspace := t.TempDir()
	additional := t.TempDir()
	root := workspace
	if isAdditional {
		root = additional
	}
	unreadable := filepath.Join(root, "forge", "data", "ssh")
	metadataPaths := []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "a", ".git"),
		filepath.Join(root, "z", ".git"),
	}
	for _, metadata := range append(slices.Clone(metadataPaths), filepath.Join(unreadable, "hidden", ".git")) {
		if err := os.MkdirAll(metadata, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	makeUnreadable(t, unreadable, mode)

	policy, err := protectedPolicy(sandbox.Policy{Write: []string{root}}, []string{root})
	if err != nil {
		return "error: " + err.Error()
	}
	return "read:\n" + renderPaths(root, policy.Read) + "\nwrite:\n" + renderPaths(root, policy.Write)
}

func temporaryGrantGolden(t *testing.T, grantedAccess Access, isAvailable bool) string {
	t.Helper()

	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	currentCaps := caps.Read | caps.Shell
	if grantedAccess.Has(WriteAccess) {
		currentCaps |= caps.Write
	}
	mode := caps.NewMode(currentCaps)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	pathAccess, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pathAccess.Close)

	temporaryFile := filepath.Join(t.TempDir(), "recording.flac")
	if err := os.WriteFile(temporaryFile, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pathAccess.Grant(temporaryFile, grantedAccess); err != nil {
		t.Fatal(err)
	}
	if !isAvailable {
		if err := os.Remove(temporaryFile); err != nil {
			t.Fatal(err)
		}
	}

	extraPaths, optionalPaths := pathAccess.getPaths()
	policy, err := createPolicyWithSupportProbe(
		context.Background(),
		workspace,
		testPlaces(t, t.TempDir(), t.TempDir()),
		extraPaths,
		optionalPaths,
		currentCaps,
		func(context.Context) error { return nil },
	)
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf(
		"read: %t\nwrite: %t\nexecute: %t\noptional: %t",
		slices.Contains(policy.Read, temporaryFile),
		slices.Contains(policy.Write, temporaryFile),
		slices.Contains(policy.Exec, temporaryFile),
		slices.Contains(policy.OptionalPaths, temporaryFile),
	)
}

func configuredPathGolden(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	configuredPath := t.TempDir()
	currentCaps := caps.Read | caps.Write | caps.Shell
	mode := caps.NewMode(currentCaps)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	pathAccess, err := NewPathAccess(files, mode, Paths{Read: []string{configuredPath}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pathAccess.Close)
	if _, err := pathAccess.Grant(configuredPath, ReadAccess|WriteAccess); err != nil {
		t.Fatal(err)
	}

	extraPaths, optionalPaths := pathAccess.getPaths()
	policy, err := createPolicyWithSupportProbe(
		context.Background(),
		workspace,
		testPlaces(t, t.TempDir(), t.TempDir()),
		extraPaths,
		optionalPaths,
		currentCaps,
		func(context.Context) error { return nil },
	)
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf(
		"read: %t\nwrite: %t\nexecute: %t\noptional: %t",
		slices.Contains(policy.Read, configuredPath),
		slices.Contains(policy.Write, configuredPath),
		slices.Contains(policy.Exec, configuredPath),
		slices.Contains(policy.OptionalPaths, configuredPath),
	)
}

func missingProtectionRootGolden(t *testing.T) string {
	t.Helper()

	parent := t.TempDir()
	missing := filepath.Join(parent, "missing")
	_, err := protectedPolicy(sandbox.Policy{}, []string{missing})
	if err == nil {
		return "error: none"
	}
	return "error: " + strings.ReplaceAll(err.Error(), missing, "/missing")
}

func renderPaths(root string, paths []string) string {
	if len(paths) == 0 {
		return "(none)"
	}

	rendered := make([]string, 0, len(paths))
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err == nil && filepath.IsLocal(relative) {
			if relative == "." {
				relative = "<root>"
			}
			rendered = append(rendered, relative)
			continue
		}
		rendered = append(rendered, path)
	}
	return strings.Join(rendered, "\n")
}

func compareShellGolden(t *testing.T, name string, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

func symlinkedWorkspaceGolden(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	metadata := filepath.Join(workspace, "nested", ".git")
	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}
	policy, err := protectedPolicy(sandbox.Policy{Write: []string{alias}}, []string{alias})
	if err != nil {
		return "error: " + err.Error()
	}
	return "read:\n" + renderPaths(workspace, policy.Read)
}

func hostTmpAliasGolden(t *testing.T) string {
	t.Helper()
	outside := outsideTmpTestDirectory(t)
	hostTemporary := hostTmpTestDirectory(t)
	alias := filepath.Join(outside, "into-tmp")
	if err := os.Symlink(hostTemporary, alias); err != nil {
		t.Fatal(err)
	}
	_, configuredError := PreparePaths(Paths{Read: []string{alias}}, io.Discard)
	_, directError := PreparePaths(Paths{Read: []string{hostTemporary}}, io.Discard)
	_, ordinaryError := PreparePaths(Paths{Read: []string{outside}}, io.Discard)
	mode := caps.NewMode(caps.Read)
	files := configuredPathTestRoot(t, mode)
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	changed, grantError := access.Grant(alias, ReadAccess)
	return fmt.Sprintf("configured alias refused: %t\ndirect host tmp refused: %t\ntemporary grant refused: %t\nordinary path accepted: %t",
		configuredError != nil, directError != nil, grantError != nil && !changed, ordinaryError == nil)
}

func yoloTmpGolden(t *testing.T) string {
	t.Helper()
	workspace := outsideTmpTestDirectory(t)
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = openedRoot.Close() })
	files := file.New(openedRoot, func(string) error { return file.ErrReadOnly })
	hostTemporary := hostTmpTestDirectory(t)
	hostFile := filepath.Join(hostTemporary, "note")
	if err := os.WriteFile(hostFile, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	scratch := outsideTmpTestDirectory(t)
	scratchFile := filepath.Join(scratch, "note")
	if err := os.WriteFile(scratchFile, []byte("scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := MountHostTemporaryDirectory(files, scratch)
	t.Cleanup(func() {
		for _, root := range roots {
			_ = root.Close()
		}
	})
	hostRoot, hostName, hostError := files.Resolve(hostFile)
	scratchRoot, scratchName, scratchError := files.Resolve(scratchFile)
	hostContent := []byte(nil)
	if hostError == nil {
		hostContent, hostError = hostRoot.ReadFile(hostName)
	}
	scratchContent := []byte(nil)
	if scratchError == nil {
		scratchContent, scratchError = scratchRoot.ReadFile(scratchName)
	}
	_, configuredError := PreparePathsForMode(Paths{Read: []string{hostTemporary}}, io.Discard, true)
	return fmt.Sprintf("host tmp is host: %t\nscratch stays reachable: %t\nhost tmp config allowed: %t",
		hostError == nil && string(hostContent) == "host", scratchError == nil && string(scratchContent) == "scratch", configuredError == nil)
}

func virtualTmpMountGolden(t *testing.T) string {
	t.Helper()
	workspace := outsideTmpTestDirectory(t)
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = openedRoot.Close() })
	files := file.New(openedRoot, func(string) error { return file.ErrReadOnly })
	scratchRoot := MountTemporaryDirectory(files, outsideTmpTestDirectory(t))
	t.Cleanup(func() { _ = scratchRoot.Close() })
	hostTemporary := hostTmpTestDirectory(t)
	unlisted := filepath.Join(outsideTmpTestDirectory(t), "unlisted")
	if err := os.Symlink(hostTemporary, unlisted); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(workspace, "inside")
	if err := os.Symlink(hostTemporary, inside); err != nil {
		t.Fatal(err)
	}
	_, _, unlistedError := files.Resolve(filepath.Join(unlisted, "note"))
	insideRoot, _, insideError := files.Resolve(filepath.Join(inside, "note"))
	directRoot, _, directError := files.Resolve(filepath.Join(hostTemporary, "note"))
	return fmt.Sprintf("unlisted alias refused: %t\nworkspace alias sees scratch: %t\ndirect tmp sees scratch: %t",
		errors.Is(unlistedError, file.ErrOutsideRoot), insideError == nil && insideRoot == scratchRoot, directError == nil && directRoot == scratchRoot)
}

func changedMountedPathGolden(t *testing.T) string {
	t.Helper()
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
	t.Cleanup(func() { _ = metadataRoot.Close() })
	if err := metadataRoot.WriteFile("config", []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = openedRoot.Close() })
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(openedRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{grant}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
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
	_, readError := mountedRoot.ReadFile(name)
	writeError := mountedRoot.WriteFile(name, []byte("changed"), 0o600)
	content, err := metadataRoot.ReadFile("config")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("read refused: %t\nwrite refused: %t\nmetadata unchanged: %t",
		errors.Is(readError, file.ErrMountChanged), errors.Is(writeError, file.ErrMountChanged), string(content) == "intact")
}

func gitWithdrawalGolden() string {
	home := "/session-home"
	stops, _ := StoppedBy(caps.Git, "/workspace")
	metadata := sandbox.Policy{Write: []string{"/workspace/.git"}, SetEnv: map[string]string{"HOME": home}}
	configured := sandbox.Policy{Write: []string{"/reference"}, SetEnv: map[string]string{"HOME": home}}
	caches := sandbox.Policy{Write: []string{filepath.Join(home, ".cache"), sandbox.TmpDir}, SetEnv: map[string]string{"HOME": home}}
	return fmt.Sprintf("metadata job stopped: %t\nconfigured writer stopped: %t\ncache-only job kept: %t",
		stops(metadata), stops(configured), !stops(caches))
}

func aliasedWriteGrantGolden(t *testing.T) string {
	t.Helper()
	workspace := outsideTmpTestDirectory(t)
	writable := filepath.Join(workspace, "writable")
	if err := os.Mkdir(writable, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "alias")
	if err := os.Symlink("writable", alias); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Write: []string{writable}, Home: []string{alias}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	return fmt.Sprintf("configured write wins equal real path: %t", files.RefuseWrite("writable/note") == nil)
}

func nestedWriteGrantGolden(t *testing.T) string {
	t.Helper()
	workspace := outsideTmpTestDirectory(t)
	readOnly := filepath.Join(workspace, "read-only")
	writable := filepath.Join(readOnly, "writable")
	if err := os.MkdirAll(writable, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(root, caps.RefuseWrite(mode))
	paths := Paths{Read: []string{readOnly}, Write: []string{writable}}
	access, err := NewPathAccess(files, mode, paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	readRefusal := files.RefuseWrite("read-only/note")
	writeRefusal := files.RefuseWrite("read-only/writable/note")
	policy, err := createTestPolicy(t, workspace, t.TempDir(), t.TempDir(), paths, mode.Current())
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("nested read refused: %t\nnested write allowed: %t\nshell keeps read-only scope: %t",
		errors.Is(readRefusal, file.ErrReadOnly), writeRefusal == nil,
		slices.Contains(policy.Read, readOnly) && !slices.Contains(policy.Write, readOnly) && slices.Contains(policy.Write, writable))
}

func workspaceWriteGrantGolden(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	mode := caps.NewMode(caps.Read | caps.Shell)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	if _, err := access.Grant(workspace, ReadAccess|WriteAccess); err != nil {
		t.Fatal(err)
	}
	paths, optional := access.getPaths()
	policy, err := createPolicyWithSupportProbe(context.Background(), workspace, testPlaces(t, t.TempDir(), t.TempDir()), paths, optional,
		mode.Current(), func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	written := files.WriteFile("note", []byte("yes"), 0o600)
	access.Revoke(workspace)
	revoked := files.WriteFile("note", []byte("no"), 0o600)
	return fmt.Sprintf("shell writable: %t\nfile tools writable: %t\nfile tools read-only after revoke: %t",
		slices.Contains(policy.Write, workspace), written == nil, errors.Is(revoked, file.ErrReadOnly))
}

func absoluteDenyGolden(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	secret := filepath.Join(workspace, "auth.json")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	mode := caps.NewMode(caps.Read)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{Deny: []string{secret}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	_, fileError := files.ReadFile("auth.json")
	policy := sandbox.Policy{Deny: []string{secret}, Read: []string{workspace}}
	walks := 0
	deniedPaths, err := policy.DiscoverDenyPathsWith(func([]string, string, string) ([]string, error) {
		walks++
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("directory walks: %d\nshell denies exact path: %t\nfile tools deny read: %t",
		walks, slices.Contains(deniedPaths, secret), errors.Is(fileError, ErrDenied))
}

func hardLinkGolden(t *testing.T) string {
	t.Helper()
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
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	before := files.WriteFile("ordinary", []byte("changed"), 0o600)
	mode.Toggle(caps.Git)
	after := files.WriteFile("ordinary", []byte("changed"), 0o600)
	content, err := files.ReadFile(".git/config")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "ordinary")); err != nil {
		t.Fatal(err)
	}
	singleLink := files.WriteFile(".git/config", []byte("allowed"), 0o600)
	return fmt.Sprintf("without g refused: %t\nwith g refused: %t\nmetadata unchanged: %t\nsingle-link metadata writable with g: %t",
		errors.Is(before, file.ErrMultipleHardLinks), errors.Is(after, file.ErrMultipleHardLinks), string(content) == "intact", singleLink == nil)
}

func gitOnlyGolden(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	mode := caps.NewMode(caps.Read | caps.Git)
	files := file.New(root, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	missing := files.MkdirAll(".git/hooks", 0o700)
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	present := files.WriteFile(".git/config", []byte("ok"), 0o600)
	ordinary := files.WriteFile("ordinary", []byte("no"), 0o600)
	home := t.TempDir()
	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{}, mode.Current())
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("missing metadata refused: %t\nexisting metadata writable: %t\nordinary file refused: %t\nshell home read-only: %t\nshell cache writable: %t",
		errors.Is(missing, file.ErrReadOnly), present == nil, errors.Is(ordinary, file.ErrReadOnly),
		!slices.Contains(policy.Write, home) && slices.Contains(policy.Read, home),
		slices.Contains(policy.Write, filepath.Join(home, ".cache")))
}
