package xdg_test

import (
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/xdg"
)

func TestStatePathUsesAnAbsoluteXDGStateHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, "somewhere", "state")
	if got := xdg.StatePath("somewhere", "state"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatePathIgnoresARelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".local", "state", "somewhere")
	if got := xdg.StatePath("somewhere"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigPathUsesAnAbsoluteXDGConfigHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, "somewhere", "SYSTEM.md")
	if got := xdg.ConfigPath("somewhere", "SYSTEM.md"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigPathIgnoresARelativeXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".config", "somewhere")
	if got := xdg.ConfigPath("somewhere"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCachePathUsesAnAbsoluteXDGCacheHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)

	want := filepath.Join(root, "somewhere", "analysis.json")
	if got := xdg.CachePath("somewhere", "analysis.json"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCachePathIgnoresARelativeXDGCacheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".cache", "somewhere")
	if got := xdg.CachePath("somewhere"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
