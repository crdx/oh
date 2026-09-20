package shell

import (
	"context"
	"fmt"
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
		"=== live read and execute grant ===\n" + temporaryGrantGolden(t, ReadAccess|ExecAccess, true),
		"=== live read and write grant ===\n" + temporaryGrantGolden(t, ReadAccess|WriteAccess, true),
		"=== vanished read and execute grant ===\n" + temporaryGrantGolden(t, ReadAccess|ExecAccess, false),
		"=== vanished read and write grant ===\n" + temporaryGrantGolden(t, ReadAccess|WriteAccess, false),
		"=== configured path with temporary write access ===\n" + configuredPathGolden(t),
		"=== missing protection root ===\n" + missingProtectionRootGolden(t),
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

	workspace := t.TempDir()
	outside := t.TempDir()
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
