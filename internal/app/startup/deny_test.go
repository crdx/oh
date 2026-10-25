package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeWorkspaceEntries(t *testing.T, root string, count int) {
	t.Helper()
	for i := range count {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("entry-%06d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDenyWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	makeWorkspaceEntries(t, root, 10)
	if err := checkDenyWorkspace(t.Context(), root, 10); err != nil {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, 9); err == nil || !strings.Contains(err.Error(), "more than 9 filesystem entries") || !strings.Contains(err.Error(), "-f/--force") {
		t.Fatalf("expected actionable refusal, got %v", err)
	}
}

func TestDenyWorkspaceCountsNestedAndHiddenEntries(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, ".hidden", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	makeWorkspaceEntries(t, nested, 3)
	if err := checkDenyWorkspace(t.Context(), root, 5); err != nil {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, 4); err == nil {
		t.Fatal("ignored nested or hidden entries")
	}
}

func TestDenyWorkspaceDoesNotFollowSymlinks(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	makeWorkspaceEntries(t, elsewhere, 10)
	if err := os.Symlink(elsewhere, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, 2); err != nil {
		t.Fatal(err)
	}
}

func TestDenyWorkspaceOnlyWalksWithNameDeniesAndShellWithoutForce(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, test := range []struct {
		name     string
		denies   []string
		hasShell bool
		isForced bool
	}{
		{name: "empty", hasShell: true},
		{name: "absolute", denies: []string{"/private/secret"}, hasShell: true},
		{name: "no shell", denies: []string{"secret"}},
		{name: "forced", denies: []string{"secret"}, hasShell: true, isForced: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := CheckDenyWorkspace(t.Context(), missing, test.denies, test.hasShell, test.isForced); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, denies := range [][]string{{"secret"}, {"*.pem"}, {"/private/secret", "secret"}} {
		if err := CheckDenyWorkspace(t.Context(), missing, denies, true, false); err == nil || !strings.Contains(err.Error(), "--force") {
			t.Fatalf("expected walk failure with override, got %v", err)
		}
	}
}

func TestDenyWorkspaceCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := CheckDenyWorkspace(ctx, t.TempDir(), []string{"secret"}, true, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestDenyWorkspaceStopsBeforeOpeningSubdirectoriesOverLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, 0); err == nil || !strings.Contains(err.Error(), "more than 0") {
		t.Fatalf("got %v", err)
	}
}

func TestDenyWorkspaceCountsAcrossDirectoryBatches(t *testing.T) {
	root := t.TempDir()
	count := denyDirectoryBatchSize + 1
	makeWorkspaceEntries(t, root, count)
	if err := checkDenyWorkspace(t.Context(), root, count); err != nil {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, count-1); err == nil {
		t.Fatal("the next directory batch was not counted")
	}
}

func TestDenyWorkspaceCountsDeepTrees(t *testing.T) {
	root := t.TempDir()
	directory := root
	const depth = 512
	for range depth {
		directory = filepath.Join(directory, "d")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkDenyWorkspace(t.Context(), root, depth); err != nil {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, depth-1); err == nil {
		t.Fatal("the deepest directory was not counted")
	}
}

func TestDenyWorkspaceIgnoresUnreadableSubdirectories(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "unreadable")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	makeWorkspaceEntries(t, hidden, 10)
	if err := os.Chmod(hidden, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hidden, 0o700) }) //nolint:gosec // Restore access to the test directory.
	if _, err := os.ReadDir(hidden); err == nil {
		t.Skip("this process can read directories without permission")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	if err := checkDenyWorkspace(t.Context(), root, 1); err != nil {
		t.Fatal(err)
	}
}
