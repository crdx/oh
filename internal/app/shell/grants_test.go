package shell

import (
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/sandbox"
)

func TestPermanentGrantsFollowLiveWorkspaceCapabilities(t *testing.T) {
	workspaceDirectory := t.TempDir()
	homeDirectory := t.TempDir()

	readOnly := PermanentGrants(workspaceDirectory, homeDirectory, Paths{}, caps.Read, false)
	assertScopedGrant(t, readOnly, WorkspaceGrant, workspaceDirectory, ReadAccess)
	assertScopedGrant(t, readOnly, PrivateHomeGrant, homeDirectory, ReadAccess)
	assertScopedGrant(t, readOnly, TemporaryDirectoryGrant, sandbox.TmpDir, ReadAccess|WriteAccess)

	writable := PermanentGrants(
		workspaceDirectory,
		homeDirectory,
		Paths{},
		caps.Read|caps.Shell|caps.Write,
		false,
	)
	assertScopedGrant(t, writable, WorkspaceGrant, workspaceDirectory, ReadAccess|ExecAccess|WriteAccess)
	assertScopedGrant(t, writable, PrivateHomeGrant, homeDirectory, ReadAccess|ExecAccess|WriteAccess)
	assertScopedGrant(
		t,
		writable,
		TemporaryDirectoryGrant,
		sandbox.TmpDir,
		ReadAccess|ExecAccess|WriteAccess,
	)
	assertScopedGrant(t, writable, RuntimeGrant, "/proc", ReadAccess)
	assertScopedGrant(t, writable, RuntimeGrant, "/dev/pts", ReadAccess|WriteAccess)
}

func TestPermanentGrantsShowRepositoryMetadataAccess(t *testing.T) {
	workspaceDirectory := t.TempDir()
	grants := PermanentGrants(workspaceDirectory, t.TempDir(), Paths{}, caps.Read|caps.Git, false)
	assertScopedGrant(
		t,
		grants,
		RepositoryMetadataGrant,
		filepath.Join(workspaceDirectory, "**", ".git"),
		ReadAccess|WriteAccess,
	)
}

func TestPermanentGrantsLabelConfiguredAndUnconfinedShellAccess(t *testing.T) {
	workspaceDirectory := t.TempDir()
	homeDirectory := t.TempDir()
	configuredDirectory := t.TempDir()
	grants := PermanentGrants(
		workspaceDirectory,
		homeDirectory,
		Paths{Exec: []string{configuredDirectory}},
		caps.Read|caps.Shell,
		true,
	)

	assertScopedGrant(t, grants, ConfiguredGrant, configuredDirectory, ReadAccess|ExecAccess)
	assertScopedGrant(
		t,
		grants,
		UnconfinedShellGrant,
		"/",
		ReadAccess|ExecAccess|WriteAccess,
	)
}

func assertScopedGrant(
	t *testing.T,
	grants []ScopedPathGrant,
	kind GrantKind,
	path string,
	access Access,
) {
	t.Helper()
	for _, grant := range grants {
		if grant.Kind == kind && grant.Path == path {
			if grant.Access != access {
				t.Errorf("%s grant for %s has %s, want %s", kind, path, grant.Access.Flags(), access.Flags())
			}
			return
		}
	}
	t.Errorf("no %s grant for %s in %#v", kind, path, grants)
}
