package pathgrant

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/agent"
)

func outsideTmpTestDirectory(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(filepath.Join(home, ".cache"), "oh-grant-test-") //nolint:usetesting // the fixture must stand outside host /tmp
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func hostTmpTestDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(sandbox.TmpDir, "oh-host-tmp-test-") //nolint:usetesting // the fixture must stand inside host /tmp
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func openTestWorkspace(t *testing.T) *work.Space {
	t.Helper()
	t.Setenv("TMPDIR", outsideTmpTestDirectory(t))

	workspace := work.At(t.TempDir())
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })

	return workspace
}

func newTestGrants(t *testing.T) (*Grants, *file.Root, *caps.Mode) {
	t.Helper()

	workspace := openTestWorkspace(t)

	mode := caps.NewMode(caps.Read)
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	return New(workspace, access), files, mode
}

func TestPublicGrantRejectsHostTmpPathsAfterCanonicalisingAliases(t *testing.T) {
	grants, files, _ := newTestGrants(t)
	hostTemporary := hostTmpTestDirectory(t)
	alias := filepath.Join(outsideTmpTestDirectory(t), "host-tmp")
	if err := os.Symlink(hostTemporary, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{hostTemporary, alias} {
		if _, err := grants.Grant(path, ReadAccess); err == nil {
			t.Errorf("public grant accepted host tmp path %s", path)
		}
	}
	if _, _, err := files.Resolve(hostTemporary); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("file tools reached host tmp through refused grants: %v", err)
	}
}

func TestAnUnconfinedGrantCanNameHostTmp(t *testing.T) {
	workspace := openTestWorkspace(t)
	mode := caps.NewMode(caps.All())
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccessForMode(files, mode, shell.Paths{}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	scratch := outsideTmpTestDirectory(t)
	roots := shell.MountHostTemporaryDirectory(files, scratch)
	t.Cleanup(func() {
		for _, root := range roots {
			_ = root.Close()
		}
	})
	hostTemporary := hostTmpTestDirectory(t)
	hostFile := filepath.Join(hostTemporary, "note")
	if err := os.WriteFile(hostFile, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	grants := New(workspace, access)
	if _, err := grants.Grant(hostTemporary, ReadAccess); err != nil {
		t.Errorf("host tmp grant was refused under yolo: %v", err)
	}
	resolvedRoot, name, err := files.Resolve(hostFile)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := resolvedRoot.ReadFile(name); err != nil || string(content) != "host" {
		t.Errorf("unconfined read got %q and %v, want host tmp", content, err)
	}
	_, restored := NewRestored(workspace, access, []Grant{{Path: hostTemporary, Access: ReadAccess}})
	if len(restored.Failures) != 0 {
		t.Errorf("restoring the host tmp grant under yolo failed: %+v", restored.Failures)
	}
}

func TestReadAndWriteGrantsReachTheFileTools(t *testing.T) {
	grants, files, mode := newTestGrants(t)
	directory := t.TempDir()
	proof := filepath.Join(directory, "proof")
	if err := os.WriteFile(proof, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	event, err := grants.Grant(directory, ReadAccess)
	if err != nil {
		t.Fatal(err)
	}
	if notice, found := Notice(event); !found || notice != "Granted temporary read-only access to "+directory+"." {
		t.Errorf("got notice %q and %t", notice, found)
	}
	mountedRoot, name, err := files.Resolve(proof)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := mountedRoot.ReadFile(name); err != nil || string(data) != "original" {
		t.Errorf("read got %q and %v", data, err)
	}
	if err := mountedRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("read grant write got %v", err)
	}

	if _, err := grants.Grant(directory, ReadAccess|WriteAccess); err != nil {
		t.Fatal(err)
	}
	mountedRoot, name, err = files.Resolve(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Errorf("write grant without write capability got %v", err)
	}

	metadata := filepath.Join(".git", "config")
	if err := mountedRoot.MkdirAll(".git", 0o700); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("write grant repository metadata without git capability got %v", err)
	}
	mode.Toggle(caps.Git)
	if err := mountedRoot.MkdirAll(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(metadata, nil, 0o600); err != nil {
		t.Errorf("write grant repository metadata with git capability got %v", err)
	}
}

func TestRevokingAGrantRemovesItFromTheFileTools(t *testing.T) {
	grants, files, _ := newTestGrants(t)
	directory := t.TempDir()
	if _, err := grants.Grant(directory, ReadAccess); err != nil {
		t.Fatal(err)
	}

	event, err := grants.Revoke(directory)
	if err != nil {
		t.Fatal(err)
	}
	if notice, found := Notice(event); !found || notice != "Revoked temporary access to "+directory+"." {
		t.Errorf("got notice %q and %t", notice, found)
	}
	if _, _, err := files.Resolve(directory); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("revoked path resolved with %v", err)
	}
}

func TestAnExactFileGrantDoesNotExposeItsSiblings(t *testing.T) {
	grants, files, _ := newTestGrants(t)
	directory := t.TempDir()
	grantedPath := filepath.Join(directory, "granted")
	siblingPath := filepath.Join(directory, "sibling")
	if err := os.WriteFile(grantedPath, []byte("granted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingPath, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := grants.Grant(grantedPath, ReadAccess); err != nil {
		t.Fatal(err)
	}
	if _, _, err := files.Resolve(grantedPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := files.Resolve(siblingPath); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("sibling resolved with %v", err)
	}
}

func TestRelativePathsResolveFromTheWorkspaceAndPathsMayContainSpaces(t *testing.T) {
	workspace := openTestWorkspace(t)
	mode := caps.NewMode(caps.Read)
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	grants := New(workspace, access)

	path := filepath.Join(workspace.GetDir(), "path with spaces")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := grants.Grant("path with spaces", ReadAccess); err != nil {
		t.Fatal(err)
	}
	if current := grants.GetCurrent(); len(current) != 1 || current[0].Path != path {
		t.Errorf("got grants %#v", current)
	}
}

func TestAHomePathIsExpandedRatherThanJoinedToTheWorkspace(t *testing.T) {
	grants, _, _ := newTestGrants(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, "granted")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := grants.Grant("~/granted", ReadAccess); err != nil {
		t.Fatal(err)
	}
	if current := grants.GetCurrent(); len(current) != 1 || current[0].Path != path {
		t.Errorf("got grants %#v", current)
	}

	if _, err := grants.Grant("~/absent", ReadAccess); err == nil ||
		!strings.Contains(err.Error(), "~/absent") {
		t.Errorf("got %v, want a failure naming the path as it was written", err)
	}
}

func TestTemporaryGrantsAcceptOnlyReadAndReadWriteAccess(t *testing.T) {
	grants, _, _ := newTestGrants(t)

	for _, access := range []Access{WriteAccess, ReadAccess | shell.ExecAccess} {
		if _, err := grants.Grant(t.TempDir(), access); err == nil ||
			!strings.Contains(err.Error(), `want some of "rw"`) {
			t.Errorf("access %q got %v", access.Flags(), err)
		}
	}

	if _, err := ChangeEvent("/reference", []Grant{{
		Path:   "/reference",
		Access: ReadAccess | shell.ExecAccess,
	}}); err == nil {
		t.Error("an executable path grant event was written")
	}
}

func TestGrantChangesAreInjectedOnce(t *testing.T) {
	grants, _, _ := newTestGrants(t)
	directory := t.TempDir()
	if _, err := grants.Grant(directory, ReadAccess|WriteAccess); err != nil {
		t.Fatal(err)
	}

	want := "Granted temporary read and write access to " + directory + "."
	if got := grants.Inject(); got != want {
		t.Errorf("got injection %q", got)
	}
	if got := grants.Inject(); got != "" {
		t.Errorf("got repeated injection %q", got)
	}
}

func TestTheLatestCompleteEventRestoresTheGrantCollection(t *testing.T) {
	firstPath := t.TempDir()
	secondPath := t.TempDir()
	first, err := ChangeEvent(firstPath, []Grant{{Path: firstPath, Access: ReadAccess}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ChangeEvent(secondPath, []Grant{{Path: secondPath, Access: ReadAccess | WriteAccess}})
	if err != nil {
		t.Fatal(err)
	}

	restored, found := LastRecorded([]agent.Event{first, {Kind: Change, State: []byte("broken")}, second})
	want := []Grant{{Path: secondPath, Access: ReadAccess | WriteAccess}}
	if !found || !reflect.DeepEqual(restored, want) {
		t.Errorf("got %#v and %t", restored, found)
	}
}

func TestARestoredHostTmpGrantIsWithdrawn(t *testing.T) {
	workspace := openTestWorkspace(t)
	mode := caps.NewMode(caps.Read)
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	hostTemporary := hostTmpTestDirectory(t)
	grants, result := NewRestored(workspace, access, []Grant{{Path: hostTemporary, Access: ReadAccess}})
	if len(result.Failures) != 1 || len(grants.GetCurrent()) != 0 {
		t.Errorf("restored host tmp with failures %+v and grants %+v", result.Failures, grants.GetCurrent())
	}
	if _, _, err := files.Resolve(hostTemporary); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("restored host tmp resolved through file tools: %v", err)
	}
}

func TestExactRestoreNeedsNoNewModelAnnouncement(t *testing.T) {
	workspace := openTestWorkspace(t)
	mode := caps.NewMode(caps.Read)
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)

	present := t.TempDir()
	grants, result := NewRestored(workspace, access, []Grant{{Path: present, Access: ReadAccess}})
	if len(result.Failures) != 0 {
		t.Fatalf("got restoration failures %#v", result.Failures)
	}
	if message := grants.Inject(); message != "" {
		t.Errorf("exact restore announced %q", message)
	}
}

func TestRestoreRefusesARecordedPathThatBecameASymlink(t *testing.T) {
	workspace := openTestWorkspace(t)
	mode := caps.NewMode(caps.Read)
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)

	recordedPath := filepath.Join(t.TempDir(), "reference")
	if err := os.Mkdir(recordedPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recordedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), recordedPath); err != nil {
		t.Fatal(err)
	}

	_, result := NewRestored(workspace, access, []Grant{{Path: recordedPath, Access: ReadAccess}})
	if len(result.Failures) != 1 || !strings.Contains(result.Failures[0].Err.Error(), "now resolves") {
		t.Errorf("got restoration failures %#v", result.Failures)
	}
}

func TestRestoreReopensPresentPathsAndCorrectsMissingOnes(t *testing.T) {
	workspace := openTestWorkspace(t)
	mode := caps.NewMode(caps.Read)
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)

	present := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	recorded := []Grant{
		{Path: missing, Access: ReadAccess},
		{Path: present, Access: ReadAccess | WriteAccess},
	}
	grants, result := NewRestored(workspace, access, recorded)
	if len(result.Failures) != 1 || result.Failures[0].Grant.Path != missing {
		t.Fatalf("got restoration failures %#v", result.Failures)
	}
	if current := grants.GetCurrent(); !reflect.DeepEqual(current, []Grant{{Path: present, Access: ReadAccess | WriteAccess}}) {
		t.Errorf("got restored grants %#v", current)
	}
	if _, _, err := files.Resolve(present); err != nil {
		t.Fatal(err)
	}
	if message := grants.Inject(); !strings.Contains(message, missing) || !strings.Contains(message, "Revoked") {
		t.Errorf("got correction %q", message)
	}
}
