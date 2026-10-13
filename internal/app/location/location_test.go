package location_test

import (
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/location"
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

func TestTheAnalysisCacheFollowsTheStateDirectory(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		stateDirectory string
		usesXDGState   bool
	}{
		{name: "home fallback"},
		{name: "XDG state", usesXDGState: true},
		{name: "absolute override", stateDirectory: "/isolated/state", usesXDGState: true},
		{name: "relative override", stateDirectory: "relative/state", usesXDGState: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", "")
			t.Setenv(location.StateDirVariable, scenario.stateDirectory)

			root := filepath.Join(home, ".local", "state")
			if scenario.usesXDGState {
				root = t.TempDir()
				t.Setenv("XDG_STATE_HOME", root)
			}
			want := filepath.Join(root, "org.crdx", "oh", "analysis.json")
			if filepath.IsAbs(scenario.stateDirectory) {
				want = filepath.Join(scenario.stateDirectory, "analysis.json")
			}
			if got := location.GetAnalysisCachePath(); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
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
