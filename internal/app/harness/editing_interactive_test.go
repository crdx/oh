package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/config"
	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/app/width"
)

const (
	pressCtrlG        = "\x07"
	pressCtrlC        = "\x03"
	pressLeft         = "\x1b[D"
	fakeEditorBanner  = "~ fake editor ~"
	resizedColumns    = 80
	leftTheEditor     = "\x1b[?1049l"
	fakeTerminalEdits = `#!/bin/sh
printf '\033[?1049h\033[H%s\r\nopened with %s\r\n' '` + fakeEditorBanner + `' "$1"
IFS= read -r line
for last; do :; done
printf '%s\n' "$line" > "$last"
printf '\033[?1049l'
`
)

func writeFakeTerminalEditor(t *testing.T) string {
	t.Helper()
	editorPath := filepath.Join(t.TempDir(), "vim")
	//nolint:gosec // the editor has to be executable
	if err := os.WriteFile(editorPath, []byte(fakeTerminalEdits), 0o755); err != nil {
		t.Fatal(err)
	}

	return editorPath
}

func newEditingSession(t *testing.T, arguments ...string) *interactiveSession {
	t.Helper()

	editorPath := writeFakeTerminalEditor(t)

	rig := newInteractiveRig(t, "An answer to quote.")
	rig.environment = append(rig.environment,
		"EDITOR="+editorPath,
		"VISUAL=",
		"DISPLAY=",
		"WAYLAND_DISPLAY=",
	)

	session := rig.start(append([]string{"--yolo", "-m", "opencode-go/fake"}, arguments...)...)
	session.waitFor(readyBanner)

	return session
}

func (self *interactiveSession) resize(columns int, rows int) {
	self.t.Helper()

	size := &unix.Winsize{Row: uint16(rows), Col: uint16(columns)} //nolint:gosec // a size a terminal can have
	if err := unix.IoctlSetWinsize(int(self.terminal.Fd()), unix.TIOCSWINSZ, size); err != nil {
		self.t.Fatal(err)
	}
}

func TestATerminalEditorEditsTheDraftInTheTerminalOhGaveIt(t *testing.T) {
	session := newEditingSession(t)
	raw := session.modes()
	session.typeAndSettle("/fork --from ")
	session.typeAndSettle(pressEscape + clearInput)

	session.typeAndSettle("a first draft")
	before := session.screen()

	session.typeText(pressLeft + pressLeft + pressLeft + pressLeft + pressLeft + pressCtrlG)
	session.waitFor(fakeEditorBanner)
	session.waitFor("opened with +call cursor(1, 9)")
	if modes := session.modes(); modes.Lflag&unix.ICANON == 0 || modes.Iflag&unix.ICRNL == 0 {
		t.Errorf("the editor was handed a raw terminal: %+v", modes)
	}

	session.typeText("a replaced draft" + pressEnter)
	session.waitFor("a replaced draft")
	session.requireHidden(fakeEditorBanner)
	session.requireHidden("a first draft")

	after := session.screen()
	want := strings.Join(before, "\n")
	want = strings.Replace(want, "a first draft", "a replaced draft", 1)
	if got := strings.Join(after, "\n"); got != want {
		t.Errorf("the screen came back as\n%s\nwant\n%s", got, want)
	}
	if modes := session.modes(); modes.Lflag != raw.Lflag || modes.Iflag != raw.Iflag {
		t.Errorf("oh took the terminal back in another mode: got %+v, want %+v", modes, raw)
	}

	session.typeAndSettle(" and more")
	session.requireShown("a replaced draft and more")

	session.typeAndSettle(clearInput)
	editorStarts := session.Count(fakeEditorBanner) + 1
	session.typeText("/conf" + pressEnter)
	session.waitForCount(fakeEditorBanner, editorStarts)
	editorEnds := session.Count(leftTheEditor) + 1
	session.typeText(fmt.Sprintf("version = %d", config.Format) + pressEnter)
	session.waitForCount(leftTheEditor, editorEnds)
	session.requireHidden(fakeEditorBanner)
	session.typeText("say something" + pressEnter)
	session.waitFor("An answer to quote.")
	editorStarts = session.Count(fakeEditorBanner) + 1
	session.typeText("/reply" + pressEnter)
	session.waitForCount(fakeEditorBanner, editorStarts)
	editorEnds = session.Count(leftTheEditor) + 1
	session.typeText("My inline reply." + pressEnter)
	session.waitForCount(leftTheEditor, editorEnds)
	session.waitFor("My inline reply.")
	session.requireHidden(fakeEditorBanner)
	session.typeText(clearInput)
	session.quit()
}

func TestAnInterruptInTheEditorReachesTheEditorAlone(t *testing.T) {
	session := newEditingSession(t)

	session.typeAndSettle("keep me")
	session.typeText(pressCtrlG)
	session.waitFor(fakeEditorBanner)
	session.typeText(pressCtrlC)

	session.waitFor("The draft was kept as it was")
	session.requireShown("keep me")
	session.typeAndSettle(" still here")
	session.requireShown("keep me still here")

	session.typeText(clearInput)
	session.quit()
}

func TestAScreenResizedUnderTheEditorIsDrawnAfresh(t *testing.T) {
	session := newEditingSession(t)

	session.typeAndSettle("draft")
	session.typeText(pressCtrlG)
	session.waitFor(fakeEditorBanner)
	session.resize(resizedColumns, interactiveRows)
	session.typeText("narrower" + pressEnter)
	session.waitFor("narrower")

	shown := session.screen()
	bottomRule := shown[len(shown)-1]
	for at := len(shown) - 1; at >= 0 && strings.TrimSpace(bottomRule) == ""; at-- {
		bottomRule = shown[at]
	}
	if drawnWidth := width.Of(strings.TrimRight(bottomRule, " ")); drawnWidth != resizedColumns {
		t.Errorf(
			"the bottom rule is %d columns wide, want %d, so the frame was not drawn afresh:\n%s",
			drawnWidth, resizedColumns, strings.Join(shown, "\n"),
		)
	}

	session.typeText(clearInput)
	session.quit()
}
