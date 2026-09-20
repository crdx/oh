package pathgrant

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/shell"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/internal/file"
	"crdx.org/io/pkg/agent"
)

func openTestWorkspace(t *testing.T) *work.Space {
	t.Helper()

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

func TestReadAndWriteGrantsReachTheFileToolsAndFollowTheWriteCapability(t *testing.T) {
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
	if err := mountedRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write grant without write capability got %v", err)
	}
	mode.Toggle(caps.Write)
	if err := mountedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatal(err)
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

func TestAnExecutableGrantIsToldToTheModelAndReadableByTheFileTools(t *testing.T) {
	grants, files, _ := newTestGrants(t)
	directory := t.TempDir()

	event, err := grants.Grant(directory, ReadAccess|ExecAccess)
	if err != nil {
		t.Fatal(err)
	}
	want := "Granted temporary read and execute access to " + directory +
		". Execution there follows the shell capability."
	if notice, found := Notice(event); !found || notice != want {
		t.Errorf("got notice %q and %t", notice, found)
	}
	if _, _, err := files.Resolve(directory); err != nil {
		t.Errorf("executable path did not resolve through file tools: %v", err)
	}
	if injection := grants.Inject(); !strings.Contains(injection, "Execution there follows the shell capability") {
		t.Errorf("got injection %q", injection)
	}
}

func TestAnAccessWithoutReadIsRefused(t *testing.T) {
	grants, _, _ := newTestGrants(t)

	if _, err := grants.Grant(t.TempDir(), WriteAccess); err == nil ||
		!strings.Contains(err.Error(), `want some of "rxw"`) {
		t.Errorf("got %v", err)
	}
}

func TestGrantChangesAreInjectedOnce(t *testing.T) {
	grants, _, _ := newTestGrants(t)
	directory := t.TempDir()
	if _, err := grants.Grant(directory, ReadAccess|WriteAccess); err != nil {
		t.Fatal(err)
	}

	want := "Granted temporary read and write access to " + directory +
		". Changes there follow the workspace write capability."
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
