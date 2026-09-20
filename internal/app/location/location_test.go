package location_test

import (
	"path/filepath"
	"testing"

	"crdx.org/io/internal/app/location"
)

func TestTheStateDirectoryFollowsTheHomeDirectoryByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv(location.StateDirVariable, "")

	want := filepath.Join(home, ".local", "state", "org.crdx", "oh", "sessions")
	if got := location.GetSessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAbsoluteStateDirectoryIsTakenOverTheHomeDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(location.StateDirVariable, root)

	if got, want := location.GetSessionsDir(), filepath.Join(root, "sessions"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := location.GetTmpDir("chewy-raven"), filepath.Join(root, "farm", "chewy-raven"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestARelativeStateDirectoryIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv(location.StateDirVariable, filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".local", "state", "org.crdx", "oh", "sessions")
	if got := location.GetSessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
