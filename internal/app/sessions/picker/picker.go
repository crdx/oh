package picker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/session"
)

const (
	animalColumn      = 20
	workspaceColumn   = 20
	modelColumn       = 20
	messageColumn     = 8
	lengthColumn      = 6
	sizeColumn        = 5
	roomForSize       = 80
	lastMessageColumn = 12
	roomForModel      = 100
	archiveKey        = 'a'
	markWidth         = 2
	unknownSize       = "—"
	belowMegabyte     = "<1M"
	megabyte          = 1 << 20
	gigabyte          = 1 << 30
)

type Session struct {
	Name             string
	WorkspaceDir     string
	StartedAt        time.Time
	TouchedAt        time.Time
	Title            string
	Model            string
	ModelID          string
	Effort           string
	MessageCount     int
	Bytes            int64
	IsRunning        bool
	IsFast           bool
	IsArchived       bool
	IsOtherWorkspace bool
}

func (self *Session) Messages() int { return self.MessageCount }

type Store struct {
	Sessions         []*Session
	ArchivedSessions []*Session
	Archive          func(*Session) (int64, error)
	Restore          func(*Session) (int64, error)
	Delete           func(*Session) error
	Read             func(*Session, int) ([]string, error)
	Measure          func(*Session) int64
}

func Choose(store Store, terminal *os.File, screen io.Writer) (*Session, error) {
	rows := &sessionList{store: store}
	rows.openFirstPopulatedView()

	chosenIndex, err := menu.Choose(rows, terminal, screen)
	if err != nil {
		return nil, err
	}

	return rows.chosen(chosenIndex)
}

type sessionList struct {
	store               Store
	sizeMeasurements    map[*Session]struct{}
	isArchivedView      bool
	isAllWorkspacesView bool
}

func (self *sessionList) Len() int { return len(self.rows()) }

func (self *sessionList) IsChoosable(index int) bool {
	storedSession := self.at(index)
	return !storedSession.IsRunning && !storedSession.IsOtherWorkspace
}

func (self *sessionList) IsReachable(index int) bool {
	return self.IsChoosable(index) || self.canRead(index)
}

func (self *sessionList) Adjust(int, int) {}

func (self *sessionList) Switch(int) bool {
	self.isArchivedView = !self.isArchivedView
	return true
}

func (self *sessionList) SwitchScope() bool {
	self.isAllWorkspacesView = !self.isAllWorkspacesView
	return true
}

func (self *sessionList) KeyboardHelp() []menu.Shortcut {
	return []menu.Shortcut{
		{Key: "tab", Description: "scope"},
		{Key: "← →", Description: "view"},
		{Key: "enter", Description: "preview"},
		{Key: "ctrl+a", Description: "archive/restore"},
		{Key: "del", Description: "delete"},
	}
}

func (self *sessionList) IsRemovable(index int) bool {
	if self.at(index).IsOtherWorkspace {
		return false
	}
	if self.isArchivedView {
		return self.store.Restore != nil
	}

	return self.store.Archive != nil && !self.at(index).IsRunning
}

func (self *sessionList) Removal(index int, keypress key.Key) (menu.Removal, bool) {
	movedSession := self.at(index)
	if movedSession.IsRunning || movedSession.IsOtherWorkspace {
		return menu.Removal{}, false
	}

	switch {
	case isArchiveKey(keypress):
		return self.archival(movedSession)
	case isDeleteKey(keypress):
		return self.deletion(index, movedSession)
	default:
		return menu.Removal{}, false
	}
}

func (self *sessionList) Preview(index int, keypress key.Key) (menu.Preview, bool) {
	if keypress.Code != key.Enter || !self.canRead(index) {
		return menu.Preview{}, false
	}

	readSession := self.at(index)

	preview := menu.Preview{
		Title: previewTitle(readSession),
		Read:  func(room int) ([]string, error) { return self.store.Read(readSession, room) },
	}
	if readSession.IsOtherWorkspace {
		preview.UnavailableLabel = "other workspace"
		preview.UnavailableMessage = "Session belongs to " + pathutil.Shorten(readSession.WorkspaceDir)
	}

	return preview, true
}

func previewTitle(readSession *Session) string {
	return sessionAnimal(readSession) + "  " + sessionTitle(readSession)
}

func isArchiveKey(keypress key.Key) bool {
	return keypress.Code == key.Rune && keypress.Value == archiveKey && keypress.Mod.Has(key.Ctrl)
}

func isDeleteKey(keypress key.Key) bool {
	return keypress.Code == key.Delete
}

func newestFirst(sessions []*Session) {
	slices.SortFunc(sessions, func(first, second *Session) int {
		if order := second.TouchedAt.Compare(first.TouchedAt); order != 0 {
			return order
		}
		return strings.Compare(second.Name, first.Name)
	})
}

func (self *sessionList) Text(index int) string {
	return self.at(index).Text()
}

func (self *Session) Text() string {
	mode := ""
	if self.IsFast {
		mode = "fast"
	}

	return strings.Join([]string{
		self.Name,
		self.Title,
		self.Model,
		self.ModelID,
		self.Effort,
		self.WorkspaceDir,
		mode,
	}, " ")
}

func (self *sessionList) ColumnHeader(room int) string {
	return sessionTable(self.agentColumn(), self.isAllWorkspacesView).Header(room)
}

func (self *sessionList) Row(index int, isChosen bool, room int) string {
	storedSession := self.at(index)
	self.measure(storedSession, room)
	line := rowInView(storedSession, isChosen, room, self.isAllWorkspacesView)

	switch {
	case isChosen && storedSession.IsOtherWorkspace:
		return style.ChosenRow.Over(line)
	case storedSession.IsOtherWorkspace:
		return style.OtherWorkspaceSession.Over(line)
	case isChosen && storedSession.IsRunning:
		return style.ChosenRunningSession.Over(line)
	case isChosen:
		return style.ChosenRow.Over(line)
	case storedSession.IsRunning:
		return style.RunningSession.Over(line)
	}

	return style.Answer.Over(line)
}

func (self *sessionList) measure(storedSession *Session, room int) {
	if self.store.Measure == nil || room > 0 && room < roomForSize {
		return
	}
	_, hasMeasurement := self.sizeMeasurements[storedSession]
	if storedSession.Bytes > 0 || hasMeasurement {
		return
	}
	if self.sizeMeasurements == nil {
		self.sizeMeasurements = make(map[*Session]struct{})
	}
	storedSession.Bytes = self.store.Measure(storedSession)
	self.sizeMeasurements[storedSession] = struct{}{}
}

func (self *sessionList) canRead(index int) bool {
	return self.store.Read != nil && !self.at(index).IsArchived
}

func (self *sessionList) archival(movedSession *Session) (menu.Removal, bool) {
	if self.isArchivedView {
		if self.store.Restore == nil {
			return menu.Removal{}, false
		}

		var restoredBytes int64

		return menu.Removal{
			Prompt:   "Press ctrl+a again to restore " + movedSession.Name,
			Progress: "Restoring…",
			Perform: func() error {
				bytes, err := self.store.Restore(movedSession)
				restoredBytes = bytes
				return err
			},
			Apply: func() { self.restore(movedSession, restoredBytes) },
		}, true
	}

	if self.store.Archive == nil {
		return menu.Removal{}, false
	}

	var archivedBytes int64

	return menu.Removal{
		Prompt:   "Press ctrl+a again to archive " + movedSession.Name,
		Progress: "Archiving…",
		Perform: func() error {
			bytes, err := self.store.Archive(movedSession)
			archivedBytes = bytes
			return err
		},
		Apply: func() { self.archive(movedSession, archivedBytes) },
	}, true
}

func (self *sessionList) deletion(index int, movedSession *Session) (menu.Removal, bool) {
	if self.store.Delete == nil {
		return menu.Removal{}, false
	}

	return menu.Removal{
		Prompt:   "Press delete again to delete " + movedSession.Name + " for good",
		Progress: "Deleting…",
		Perform:  func() error { return self.store.Delete(movedSession) },
		Apply:    func() { self.forget(index) },
	}, true
}

func (self *sessionList) forget(index int) {
	movedSession := self.at(index)
	if self.isArchivedView {
		self.store.ArchivedSessions = deleteSession(self.store.ArchivedSessions, movedSession)
		return
	}

	self.store.Sessions = deleteSession(self.store.Sessions, movedSession)
}

func deleteSession(sessions []*Session, deletedSession *Session) []*Session {
	index := slices.Index(sessions, deletedSession)
	if index < 0 {
		return sessions
	}

	return slices.Delete(sessions, index, index+1)
}

func (self *sessionList) restore(movedSession *Session, restoredBytes int64) {
	movedSession.IsArchived = false
	movedSession.Bytes = restoredBytes
	self.store.ArchivedSessions = deleteSession(self.store.ArchivedSessions, movedSession)
	self.store.Sessions = append(self.store.Sessions, movedSession)
	newestFirst(self.store.Sessions)
}

func (self *sessionList) archive(movedSession *Session, archivedBytes int64) {
	movedSession.IsArchived = true
	movedSession.Bytes = archivedBytes
	self.store.Sessions = deleteSession(self.store.Sessions, movedSession)
	self.store.ArchivedSessions = append(self.store.ArchivedSessions, movedSession)
	newestFirst(self.store.ArchivedSessions)
}

func (self *sessionList) rows() []*Session {
	sessions := self.store.Sessions
	if self.isArchivedView {
		sessions = self.store.ArchivedSessions
	}
	if self.isAllWorkspacesView {
		return sessions
	}

	return slices.DeleteFunc(slices.Clone(sessions), func(storedSession *Session) bool {
		return storedSession.IsOtherWorkspace
	})
}

func (self *sessionList) openFirstPopulatedView() {
	if self.Len() > 0 {
		return
	}

	self.isArchivedView = true
	if self.Len() > 0 {
		return
	}

	self.isArchivedView = false
	self.isAllWorkspacesView = true
	if self.Len() > 0 {
		return
	}

	self.isArchivedView = true
}

func (self *sessionList) at(index int) *Session { return self.rows()[index] }

func (self *sessionList) agentColumn() string {
	qualifiers := make([]string, 0, 2)
	if self.isAllWorkspacesView {
		qualifiers = append(qualifiers, "all")
	}
	if self.isArchivedView {
		qualifiers = append(qualifiers, "archived")
	}
	if len(qualifiers) == 0 {
		return "Agent"
	}

	return "Agent (" + strings.Join(qualifiers, " ") + ")"
}

func (self *sessionList) chosen(index int) (*Session, error) {
	chosenSession := self.at(index)
	if chosenSession.IsOtherWorkspace {
		return nil, fmt.Errorf("session %s belongs to another workspace", chosenSession.Name)
	}
	if !chosenSession.IsArchived {
		return chosenSession, nil
	}

	if _, err := self.store.Restore(chosenSession); err != nil {
		return nil, err
	}
	chosenSession.IsArchived = false

	return chosenSession, nil
}

func sessionTable(agentTitle string, isWorkspaceShown bool) *table.Table {
	columns := []table.Column{
		{Title: strings.Repeat(" ", markWidth) + agentTitle, Width: markWidth + animalColumn},
	}
	if isWorkspaceShown {
		columns = append(columns, table.Column{Title: "Workspace", Width: workspaceColumn})
	}
	columns = append(columns,
		table.Column{Title: "Title", IsFlex: true},
		table.Column{Title: "Model", Width: modelColumn, MinRoom: roomForModel},
		table.Column{Title: "Effort", Width: menu.EffortColumn, Align: table.Right, MinRoom: roomForModel},
		table.Column{Title: "Messages", Width: messageColumn, Align: table.Right},
		table.Column{Title: "Size", Width: sizeColumn, Align: table.Right, MinRoom: roomForSize},
		table.Column{Title: "Length", Width: lengthColumn, Align: table.Right},
		table.Column{Title: "Last Message", Width: lastMessageColumn, Align: table.Right},
	)

	return table.New(columns...)
}

func row(storedSession *Session, isChosen bool, room int) string {
	return rowInView(storedSession, isChosen, room, false)
}

func rowInView(storedSession *Session, isChosen bool, room int, isWorkspaceShown bool) string {
	cells := []string{menu.Mark(isChosen) + " " + sessionAnimal(storedSession)}
	if isWorkspaceShown {
		cells = append(cells, filepath.Base(storedSession.WorkspaceDir))
	}
	cells = append(cells,
		sessionTitle(storedSession),
		sessionModel(storedSession),
		storedSession.Effort,
		strconv.Itoa(storedSession.Messages()),
		FormatSize(storedSession.Bytes),
		util.CoarseDuration(storedSession.TouchedAt.Sub(storedSession.StartedAt)),
		util.Ago(storedSession.TouchedAt),
	)

	return sessionTable("Agent", isWorkspaceShown).Row(cells, room)
}

func sessionModel(storedSession *Session) string {
	name := strutil.OrDash(storedSession.Model)
	if storedSession.IsFast {
		return fastMode.GetMark(true) + " " + name
	}

	return name
}

func FormatSize(bytes int64) string {
	switch {
	case bytes <= 0:
		return unknownSize
	case bytes < megabyte:
		return belowMegabyte
	}

	megabytes := nearest(bytes, megabyte)
	if megabytes < gigabyte/megabyte {
		return strconv.FormatInt(megabytes, 10) + "M"
	}

	return strconv.FormatInt(nearest(bytes, gigabyte), 10) + "G"
}

func nearest(bytes int64, unit int64) int64 {
	return (bytes + unit/2) / unit
}

func sessionAnimal(storedSession *Session) string {
	emoji := session.Emoji(storedSession.Name)
	if emoji == "" {
		return storedSession.Name
	}

	return emoji + " " + storedSession.Name
}

func sessionTitle(storedSession *Session) string {
	if storedSession.Title == "" {
		return "(untitled)"
	}

	return strings.ReplaceAll(storedSession.Title, "\n", " ")
}
