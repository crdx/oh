package picker

import (
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/internal/app/key"
	"crdx.org/io/internal/app/menu"
	"crdx.org/io/internal/app/segment/fastMode"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/table"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/session"
)

const (
	animalColumn      = 20
	modelColumn       = 20
	messageColumn     = 8
	lengthColumn      = 6
	lastMessageColumn = 12
	roomForModel      = 100
	archiveKey        = 'a'
	markWidth         = 2
)

type Session struct {
	Name         string
	WorkspaceDir string
	StartedAt    time.Time
	TouchedAt    time.Time
	Title        string
	Model        string
	ModelID      string
	Effort       string
	MessageCount int
	IsRunning    bool
	IsFast       bool
	IsArchived   bool
}

func (self *Session) Messages() int { return self.MessageCount }

type Store struct {
	Sessions         []*Session
	ArchivedSessions []*Session
	Archive          func(*Session) error
	Restore          func(*Session) error
	Delete           func(*Session) error
	Read             func(*Session, int) ([]string, error)
}

func Choose(store Store, terminal *os.File, screen io.Writer) (*Session, error) {
	rows := &sessionList{store: store}

	chosenIndex, err := menu.Choose(rows, terminal, screen)
	if err != nil {
		return nil, err
	}

	return rows.chosen(chosenIndex)
}

type sessionList struct {
	store          Store
	isArchivedView bool
}

func (self *sessionList) Len() int { return len(self.rows()) }

func (self *sessionList) IsChoosable(index int) bool { return !self.at(index).IsRunning }

func (self *sessionList) IsReachable(index int) bool {
	return self.IsChoosable(index) || self.canRead(index)
}

func (self *sessionList) Adjust(int, int) {}

func (self *sessionList) Switch(int) bool {
	self.isArchivedView = !self.isArchivedView
	return true
}

func (self *sessionList) IsRemovable(index int) bool {
	if self.isArchivedView {
		return self.store.Restore != nil
	}

	return self.store.Archive != nil && !self.at(index).IsRunning
}

func (self *sessionList) Removal(index int, keypress key.Key) (menu.Removal, bool) {
	movedSession := self.at(index)
	if movedSession.IsRunning {
		return menu.Removal{}, false
	}

	switch {
	case isArchiveKey(keypress):
		return self.archival(index, movedSession)
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

	return menu.Preview{
		Title: previewTitle(readSession),
		Read:  func(room int) ([]string, error) { return self.store.Read(readSession, room) },
	}, true
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
		mode,
	}, " ")
}

func (self *sessionList) ColumnHeader(room int) string {
	return sessionTable(self.agentColumn()).Header(room)
}

func (self *sessionList) Row(index int, isChosen bool, room int) string {
	storedSession := self.at(index)
	line := row(storedSession, isChosen, room)

	switch {
	case isChosen && storedSession.IsRunning:
		return style.ChosenRunningSession.Over(line)
	case isChosen:
		return style.ChosenRow.Over(line)
	case storedSession.IsRunning:
		return style.RunningSession.Over(line)
	}

	return style.Answer.Over(line)
}

func (self *sessionList) canRead(index int) bool {
	return self.store.Read != nil && !self.at(index).IsArchived
}

func (self *sessionList) archival(index int, movedSession *Session) (menu.Removal, bool) {
	if self.isArchivedView {
		if self.store.Restore == nil {
			return menu.Removal{}, false
		}

		return menu.Removal{
			Prompt:   "Press ctrl+a again to restore " + movedSession.Name,
			Progress: "Restoring…",
			Perform:  func() error { return self.store.Restore(movedSession) },
			Apply:    func() { self.restore(index, movedSession) },
		}, true
	}

	if self.store.Archive == nil {
		return menu.Removal{}, false
	}

	return menu.Removal{
		Prompt:   "Press ctrl+a again to archive " + movedSession.Name,
		Progress: "Archiving…",
		Perform:  func() error { return self.store.Archive(movedSession) },
		Apply:    func() { self.archive(index, movedSession) },
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
	if self.isArchivedView {
		self.store.ArchivedSessions = slices.Delete(self.store.ArchivedSessions, index, index+1)
		return
	}

	self.store.Sessions = slices.Delete(self.store.Sessions, index, index+1)
}

func (self *sessionList) restore(index int, movedSession *Session) {
	movedSession.IsArchived = false
	self.store.ArchivedSessions = slices.Delete(self.store.ArchivedSessions, index, index+1)
	self.store.Sessions = append(self.store.Sessions, movedSession)
	newestFirst(self.store.Sessions)
}

func (self *sessionList) archive(index int, movedSession *Session) {
	movedSession.IsArchived = true
	self.store.Sessions = slices.Delete(self.store.Sessions, index, index+1)
	self.store.ArchivedSessions = append(self.store.ArchivedSessions, movedSession)
	newestFirst(self.store.ArchivedSessions)
}

func (self *sessionList) rows() []*Session {
	if self.isArchivedView {
		return self.store.ArchivedSessions
	}

	return self.store.Sessions
}

func (self *sessionList) at(index int) *Session { return self.rows()[index] }

func (self *sessionList) agentColumn() string {
	if self.isArchivedView {
		return "Agent (archived)"
	}

	return "Agent"
}

func (self *sessionList) chosen(index int) (*Session, error) {
	chosenSession := self.at(index)
	if !chosenSession.IsArchived {
		return chosenSession, nil
	}

	if err := self.store.Restore(chosenSession); err != nil {
		return nil, err
	}
	chosenSession.IsArchived = false

	return chosenSession, nil
}

func sessionTable(agentTitle string) *table.Table {
	return table.New(
		table.Column{Title: strings.Repeat(" ", markWidth) + agentTitle, Width: markWidth + animalColumn},
		table.Column{Title: "Title", IsFlex: true},
		table.Column{Title: "Model", Width: modelColumn, MinRoom: roomForModel},
		table.Column{Title: "Effort", Width: menu.EffortColumn, Align: table.Right, MinRoom: roomForModel},
		table.Column{Title: "Messages", Width: messageColumn, Align: table.Right},
		table.Column{Title: "Length", Width: lengthColumn, Align: table.Right},
		table.Column{Title: "Last Message", Width: lastMessageColumn, Align: table.Right},
	)
}

func row(storedSession *Session, isChosen bool, room int) string {
	return sessionTable("Agent").Row([]string{
		menu.Mark(isChosen) + " " + sessionAnimal(storedSession),
		sessionTitle(storedSession),
		sessionModel(storedSession),
		storedSession.Effort,
		strconv.Itoa(storedSession.Messages()),
		util.CoarseDuration(storedSession.TouchedAt.Sub(storedSession.StartedAt)),
		util.Ago(storedSession.TouchedAt),
	}, room)
}

func sessionModel(storedSession *Session) string {
	name := strutil.OrDash(storedSession.Model)
	if storedSession.IsFast {
		return fastMode.GetMark(true) + " " + name
	}

	return name
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
