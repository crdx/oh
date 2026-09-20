package cli

import (
	"runtime/debug"
	"testing"
)

func TestVersionIsNeverEmpty(t *testing.T) {
	if Version() == "" {
		t.Error("expected a version, got nothing")
	}
}

func TestBuildVersionFallsBackWhenBuildInformationHasNoVersion(t *testing.T) {
	for _, test := range []struct {
		name        string
		info        *debug.BuildInfo
		isAvailable bool
	}{
		{name: "unavailable"},
		{name: "nil", isAvailable: true},
		{name: "empty", info: &debug.BuildInfo{}, isAvailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersion(test.info, test.isAvailable); got != "unknown" {
				t.Errorf("got %q, want unknown", got)
			}
		})
	}
}

func TestBuildVersionReturnsTheEmbeddedVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	if got := buildVersion(info, true); got != "v1.2.3" {
		t.Errorf("got %q, want v1.2.3", got)
	}
}
