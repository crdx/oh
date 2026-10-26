package harness

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
)

func TestReloadingRequestTimeoutsChangesTheExistingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[experimental]\nresponse_timeout = \"1m\"\nstream_idle_timeout = \"2m\"\n")
	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)
	source := streamTimeoutsFrom(self.experimental)
	original := source()
	if original != (req.StreamTimeouts{Response: time.Minute, Idle: 2 * time.Minute}) {
		t.Fatalf("initial timeouts are %+v", original)
	}
	writeLiveConfig(t, path, "[experimental]\nresponse_timeout = \"3m\"\nstream_idle_timeout = \"4m\"\n")
	settleLiveConfig(t, self)
	if got := source(); got != (req.StreamTimeouts{Response: 3 * time.Minute, Idle: 4 * time.Minute}) {
		t.Errorf("reloaded timeouts are %+v", got)
	}
	writeLiveConfig(t, path, "")
	settleLiveConfig(t, self)
	if got := source(); got != (req.StreamTimeouts{Response: 10 * time.Minute, Idle: 10 * time.Minute}) {
		t.Errorf("default timeouts are %+v", got)
	}
}
