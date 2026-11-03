package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnInteractiveRevocationResolvesARelativePathThroughItsSource(t *testing.T) {
	rig := newInteractiveRig(t)
	reference := filepath.Join(rig.workspace, "reference")
	if err := os.Mkdir(reference, 0o700); err != nil {
		t.Fatal(err)
	}
	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.waitFor(readyBanner)
	session.typeText("/grant r " + reference + pressEnter)
	session.waitFor("Granted temporary read-only access to ")
	session.typeAndSettle("/revoke reference" + pressEnter)
	session.requireHidden("Granted temporary read-only access to ")
	session.typeText("/revoke reference" + pressEnter)
	session.waitFor(`No revocable target "reference"`)
	session.typeAndSettle(pressEscape)
	session.typeAndSettle(clearInput)
	session.quit()
}
