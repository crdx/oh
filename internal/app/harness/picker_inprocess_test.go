package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/session"
)

func TestGoldenThePickerListsStoredAndArchivedSessionsInProcess(t *testing.T) {
	workspace := reachableWorkspaceDir(t)
	sessionsDirectory := filepath.Join(t.TempDir(), "org.crdx", "oh", "sessions")
	if err := os.MkdirAll(sessionsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	threeHoursAgo := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	for _, name := range []string{"able-ant", "zany-zebra", "bold-bee", "tidy-tiger"} {
		writeStoredSession(t, sessionsDirectory, workspace, name, threeHoursAgo)
	}
	for _, name := range []string{"bold-bee", "tidy-tiger"} {
		if err := session.Archive(sessionsDirectory, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Not-A-Name", "odd-owl.tgz"} {
		if err := os.Mkdir(filepath.Join(sessionsDirectory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Bad-Name.tgz", "loose-file", "calm-cat.tar"} {
		if err := os.WriteFile(filepath.Join(sessionsDirectory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	picker, closePicker := runInProcessPicker(t, sessionsDirectory, workspace)
	waitForPTYText(t, picker, "zany-zebra")
	storedScreen := pickerScreen(t, picker)
	picker.typeAndSettle(t, "\x1b[C")
	waitForPTYText(t, picker, "tidy-tiger")
	archivedScreen := pickerScreen(t, picker)
	closePicker()

	compareWithGolden(t, "picker-listing", ".screen", map[string]func() string{
		"stored":   func() string { return storedScreen },
		"archived": func() string { return archivedScreen },
	})
}

func TestGoldenThePickerArchivesRestoresAndDeletesAnotherWorkspacesSessionInProcess(t *testing.T) {
	const (
		pressTab       = "\t"
		pressHome      = "\x1b[H"
		pressDown      = "\x1b[B"
		pressRight     = "\x1b[C"
		pressLeft      = "\x1b[D"
		pressDelete    = "\x1b[3~"
		pressArchive   = "a"
		keyHint        = "archive/restore"
		workspaceName  = "able-ant"
		otherName      = "far-fox"
		runningName    = "busy-bee"
		archivedName   = "odd-owl"
		otherWorkspace = "elsewhere"
	)

	workspace := reachableWorkspaceDir(t)
	stateDirectory := filepath.Join(t.TempDir(), "org.crdx", "oh")
	t.Setenv(location.StateDirVariable, stateDirectory)
	sessionsDirectory := filepath.Join(stateDirectory, "sessions")
	farmDirectory := filepath.Join(stateDirectory, "farm")
	elsewhere := filepath.Join(t.TempDir(), otherWorkspace)
	for _, directory := range []string{
		sessionsDirectory,
		elsewhere,
		filepath.Join(farmDirectory, otherName),
		filepath.Join(farmDirectory, runningName),
		filepath.Join(farmDirectory, archivedName),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	hoursAgo := func(hours int) string {
		return time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	}
	writeStoredSession(t, sessionsDirectory, elsewhere, runningName, hoursAgo(1))
	writeStoredSession(t, sessionsDirectory, elsewhere, otherName, hoursAgo(2))
	writeReadableJournal(t, sessionsDirectory, otherName, "tidy the fox den")
	writeStoredSession(t, sessionsDirectory, workspace, workspaceName, hoursAgo(3))
	writeStoredSession(t, sessionsDirectory, elsewhere, archivedName, hoursAgo(4))
	if err := session.Archive(sessionsDirectory, archivedName); err != nil {
		t.Fatal(err)
	}
	runningLock, err := session.AcquireLock(sessionsDirectory, runningName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runningLock.Release() })

	archivePath := session.ArchivePath(sessionsDirectory, otherName)
	storedPath := session.Dir(sessionsDirectory, otherName)
	otherArchivePath := session.ArchivePath(sessionsDirectory, archivedName)
	onDisk := func() string {
		var listed []string
		for _, directory := range []string{sessionsDirectory, farmDirectory} {
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				listed = append(listed, filepath.Base(directory)+"/"+entry.Name())
			}
		}
		return strings.Join(listed, "\n")
	}
	isPresent := func(path string) bool {
		_, err := os.Lstat(path)
		return err == nil
	}
	waitOnDisk := func(isDone func() bool) {
		t.Helper()
		deadline := time.Now().Add(interactiveDeadline)
		for !isDone() {
			if time.Now().After(deadline) {
				t.Fatalf("the picker never changed the disk, which holds:\n%s", onDisk())
			}
			time.Sleep(interactiveQuiet)
		}
	}

	picker, closePicker := runInProcessPicker(t, sessionsDirectory, workspace)
	defer closePicker()
	belongsTo := regexp.MustCompile(`(Session belongs to ).*`)
	passes := map[string]func() string{}
	record := func(pass string) {
		t.Helper()
		screen := belongsTo.ReplaceAllString(pickerScreen(t, picker), "${1}<elsewhere>")
		disk := onDisk()
		passes[pass] = func() string { return screen + "\n--- disk ---\n" + disk }
	}
	confirm := func(keypress string, isDone func() bool) {
		t.Helper()
		redrawn := picker.Count(keyHint) + 1
		if _, err := picker.typing.WriteString(keypress); err != nil {
			t.Fatal(err)
		}
		waitOnDisk(isDone)
		if !picker.WaitForCount(keyHint, redrawn, interactiveDeadline) {
			t.Fatalf("picker failed to redraw after confirmation: %s", picker.String())
		}
		picker.WaitToSettle(interactiveQuiet)
	}

	waitForPTYText(t, picker, workspaceName)
	record("01 this workspace alone")

	picker.typeAndSettle(t, pressTab)
	waitForPTYText(t, picker, "Agent (all)")
	picker.typeAndSettle(t, pressHome)
	if _, err := picker.typing.WriteString(pressArchive + pressArchive + pressDelete + pressDelete); err != nil {
		t.Fatal(err)
	}
	picker.typeAndSettle(t, pressDown)
	for _, prompt := range []string{"again to archive " + runningName, "again to delete " + runningName} {
		if picker.Count(prompt) != 0 {
			t.Errorf("another workspace's running session was offered %q", prompt)
		}
	}
	record("02 another workspace's running session is offered neither archiving nor deletion")

	picker.typeAndSettle(t, pressArchive)
	waitForPTYText(t, picker, "Press a again to archive "+otherName)
	record("03 archiving another workspace's session asks first")
	confirm(pressArchive, func() bool { return isPresent(archivePath) && !isPresent(storedPath) })
	record("04 another workspace's session archived")

	picker.typeAndSettle(t, pressRight)
	picker.typeAndSettle(t, pressHome)
	picker.typeAndSettle(t, pressArchive)
	waitForPTYText(t, picker, "Press a again to restore "+otherName)
	record("05 restoring another workspace's archive asks first")
	confirm(pressArchive, func() bool { return !isPresent(archivePath) && isPresent(storedPath) })
	record("06 another workspace's archive restored")

	picker.typeAndSettle(t, pressHome)
	picker.typeAndSettle(t, pressDelete)
	waitForPTYText(t, picker, "Press delete again to delete "+archivedName)
	record("07 deleting another workspace's archive asks first")
	confirm(pressDelete, func() bool {
		return !isPresent(otherArchivePath) && !isPresent(filepath.Join(farmDirectory, archivedName))
	})
	record("08 another workspace's archive deleted with its scratch")

	picker.typeAndSettle(t, pressLeft)
	picker.typeAndSettle(t, pressHome)
	picker.typeAndSettle(t, pressDown)
	picker.typeAndSettle(t, pressEnter)
	waitForPTYText(t, picker, "Session belongs to")
	record("09 another workspace's session is read but not opened")
	picker.typeAndSettle(t, pressEnter)
	record("10 another workspace's session stays closed to a second enter")
	picker.typeAndSettle(t, pressEscape)

	picker.typeAndSettle(t, pressDelete)
	waitForPTYText(t, picker, "Press delete again to delete "+otherName)
	record("11 deleting another workspace's session asks first")
	confirm(pressDelete, func() bool {
		return !isPresent(storedPath) && !isPresent(filepath.Join(farmDirectory, otherName))
	})
	record("12 another workspace's session deleted with its scratch")

	picker.typeAndSettle(t, pressTab)
	record("13 this workspace's session left alone")

	compareWithGolden(t, "picker-other-workspace", ".screen", passes)
	closePicker()
}

type inProcessPicker struct {
	*ptytest.Transcript

	typing *os.File
	result chan error
}

func runInProcessPicker(t *testing.T, directory string, workspace string) (*inProcessPicker, func()) {
	t.Helper()
	controller, terminal := ptytest.OpenSized(t, interactiveColumns, interactiveRows)
	transcript := ptytest.Record(controller)
	picker := &inProcessPicker{Transcript: transcript, typing: controller, result: make(chan error, 1)}
	go func() {
		_, err := sessions.Choose(directory, work.At(workspace), money.Dollar(), terminal, terminal)
		picker.result <- err
	}()
	var closeOnce sync.Once
	return picker, func() {
		t.Helper()
		closeOnce.Do(func() {
			if _, err := picker.typing.WriteString("\x03"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-picker.result:
				if err != nil {
					t.Errorf("picker returned %v", err)
				}
			case <-time.After(interactiveDeadline):
				t.Errorf("picker did not exit; terminal drew:\n%s", picker.String())
			}
		})
	}
}

func (self *inProcessPicker) typeAndSettle(t *testing.T, text string) {
	t.Helper()
	from := self.Len()
	if _, err := self.typing.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if !self.WaitForGrowth(from, interactiveDeadline) {
		t.Fatalf("typing %q drew nothing; terminal output:\n%s", text, self.String())
	}
	self.WaitToSettle(interactiveQuiet)
}

func pickerScreen(t *testing.T, picker *inProcessPicker) string {
	t.Helper()
	drawn := terminalQueries.ReplaceAllString(picker.String(), "")
	return strings.Join(playScreenOfSize(t, drawn, interactiveColumns, interactiveRows).text(), "\n")
}

func waitForPTYText(t *testing.T, picker *inProcessPicker, text string) {
	t.Helper()
	if !picker.WaitForCount(text, 1, interactiveDeadline) {
		t.Fatalf("%q was not drawn; terminal output:\n%s", text, picker.String())
	}
	picker.WaitToSettle(interactiveQuiet)
}
