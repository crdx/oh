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
		"=== explicit workspace write grant ===\n" + workspaceWriteGrantGolden(t),
		"=== absolute denied home secret ===\n" + absoluteDenyGolden(t),
		"=== host tmp aliases refused ===\n" + hostTmpAliasGolden(t),
		"=== virtual tmp mount attribution ===\n" + virtualTmpMountGolden(t),
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
		t.TempDir(),
		t.TempDir(),
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
		t.TempDir(),
		t.TempDir(),
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
	policy, err := createPolicyWithSupportProbe(context.Background(), workspace, t.TempDir(), t.TempDir(), paths, optional,
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
	return fmt.Sprintf("missing metadata refused: %t\nexisting metadata writable: %t\nordinary file refused: %t",
		errors.Is(missing, file.ErrReadOnly), present == nil, errors.Is(ordinary, file.ErrReadOnly))
}
