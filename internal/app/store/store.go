package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"

	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/store/transcript"
	"crdx.org/oh/internal/app/store/wire"
)

type Meta struct {
	Model        string                 `json:"model"`
	WorkspaceDir string                 `json:"workspaceDir"`
	Provider     string                 `json:"provider"`
	Effort       string                 `json:"effort,omitempty"`
	IsFast       bool                   `json:"fast,omitempty"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	Tools        []string               `json:"tools,omitempty"`
	Conditions   *conditions.Conditions `json:"conditions,omitempty"`
	Yolo         bool                   `json:"yolo,omitempty"`
}

type listingData struct {
	WorkspaceDir string `json:"workspaceDir"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	Effort       string `json:"effort,omitempty"`
	IsFast       bool   `json:"fast,omitempty"`
}

func encodeMeta(meta Meta) (json.RawMessage, json.RawMessage, error) {
	journalMeta, err := json.Marshal(meta)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(listingData{
		WorkspaceDir: meta.WorkspaceDir,
		Provider:     meta.Provider,
		Model:        meta.Model,
		Effort:       meta.Effort,
		IsFast:       meta.IsFast,
	})
	if err != nil {
		return nil, nil, err
	}
	return journalMeta, data, nil
}

const (
	transcriptName = "chat.md"
	wireName       = "wire.http"
)

type canonicalWriter interface {
	canonicalAppender
	canonicalSession
}

type canonicalAppender interface {
	Event(event agent.Event) (time.Time, error)
	Item(item json.RawMessage) error
	CompleteTurn(summary session.TurnSummary) error
}

type canonicalSession interface {
	Name() string
	ID() string
	JournalMeta() json.RawMessage
	Started() time.Time
	IsPersisted() bool
	EnsurePersisted() error
	SetMeta(journalMeta json.RawMessage, listingData json.RawMessage) error
	Close() error
}

type Writer struct {
	innerWriter canonicalWriter
	writerMutex sync.Mutex

	eventBuffer []agent.Event
	directory   string
	meta        Meta

	recorderMutex sync.Mutex

	transcriptLoggingEnabled bool
	transcriptRecorder       *transcript.Recorder

	wireRecordingEnabled bool
	wireRecorder         *wire.Recorder

	warnings []error
}

func Create(directory string, meta Meta) (*Writer, error) {
	journalMeta, data, err := encodeMeta(meta)
	if err != nil {
		return nil, err
	}
	innerWriter, err := session.Create(directory, journalMeta, data)
	if err != nil {
		return nil, err
	}
	return &Writer{
		innerWriter:              innerWriter,
		directory:                directory,
		meta:                     meta,
		transcriptLoggingEnabled: true,
		wireRecordingEnabled:     true,
	}, nil
}

func Open(directory string, name string) (*Writer, error) {
	innerWriter, err := session.Open(directory, name)
	if err != nil {
		return nil, err
	}

	var meta Meta
	if encodedMeta := innerWriter.JournalMeta(); len(encodedMeta) > 0 {
		if err := json.Unmarshal(encodedMeta, &meta); err != nil {
			_ = innerWriter.Close()
			return nil, err
		}
	}

	writer := &Writer{
		innerWriter:              innerWriter,
		directory:                directory,
		meta:                     meta,
		transcriptLoggingEnabled: true,
		wireRecordingEnabled:     true,
	}
	writer.startRecorders()
	return writer, nil
}

func (self *Writer) Event(event agent.Event) error {
	for _, releasedEvent := range self.release(event) {
		if err := self.appendEvent(releasedEvent); err != nil {
			return err
		}
	}

	return nil
}

func (self *Writer) Item(item json.RawMessage) error {
	self.writerMutex.Lock()
	err := self.innerWriter.Item(item)
	self.writerMutex.Unlock()
	if err != nil {
		return err
	}
	self.startRecorders()
	return nil
}

func (self *Writer) CompleteTurn(summary session.TurnSummary) error {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	return self.innerWriter.CompleteTurn(summary)
}

func (self *Writer) Name() string { return self.innerWriter.Name() }

func (self *Writer) ID() string { return self.innerWriter.ID() }

func (self *Writer) SetMeta(meta Meta) error {
	journalMeta, data, err := encodeMeta(meta)
	if err != nil {
		return err
	}
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	if err := self.innerWriter.SetMeta(journalMeta, data); err != nil {
		return err
	}
	self.meta = meta
	return nil
}

func (self *Writer) IsPersisted() bool {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	return self.innerWriter.IsPersisted()
}

func (self *Writer) EnsurePersisted() error {
	self.writerMutex.Lock()
	err := self.innerWriter.EnsurePersisted()
	self.writerMutex.Unlock()
	if err != nil {
		return err
	}
	self.startRecorders()
	return nil
}

func (self *Writer) Observer() req.Observer { return writerObserver{writer: self} }

func (self *Writer) TakeWarnings() []error {
	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

func (self *Writer) Close() error {
	self.writerMutex.Lock()
	canonicalError := self.innerWriter.Close()
	self.writerMutex.Unlock()
	self.recorderMutex.Lock()
	transcriptRecorder := self.transcriptRecorder
	wireRecorder := self.wireRecorder
	self.transcriptRecorder = nil
	self.wireRecorder = nil
	self.recorderMutex.Unlock()
	if transcriptRecorder != nil {
		if err := transcriptRecorder.Close(); err != nil {
			self.queueWarning(fmt.Errorf("chat.md recording disabled: %w", err))
		}
	}
	if wireRecorder != nil {
		_ = wireRecorder.Close()
	}
	return canonicalError
}

func (self *Writer) release(event agent.Event) []agent.Event {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()

	if !self.innerWriter.IsPersisted() && !isSaidByThePerson(event.Kind) {
		self.eventBuffer = append(self.eventBuffer, event)
		return nil
	}

	releasedEvents := self.eventBuffer
	self.eventBuffer = nil

	return append(releasedEvents, event)
}

func isSaidByThePerson(kind agent.Kind) bool {
	return kind == agent.UserMessageEvent || kind == hostcommand.Ran
}

func (self *Writer) appendEvent(event agent.Event) error {
	self.writerMutex.Lock()
	at, err := self.innerWriter.Event(event)
	self.writerMutex.Unlock()
	if err != nil {
		return err
	}
	self.startRecorders()

	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	if self.transcriptRecorder != nil {
		if err := self.transcriptRecorder.Event(at, event); err != nil {
			_ = self.transcriptRecorder.Close()
			self.transcriptRecorder = nil
			self.transcriptLoggingEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		}
	}
	return nil
}

func (self *Writer) startRecorders() {
	self.writerMutex.Lock()
	err := self.innerWriter.EnsurePersisted()
	startedAt := self.innerWriter.Started()
	self.writerMutex.Unlock()
	if err != nil {
		return
	}

	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	bundleDirectory := filepath.Join(self.directory, self.Name())
	if self.transcriptRecorder == nil && self.transcriptLoggingEnabled {
		recorder, err := transcript.Open(filepath.Join(bundleDirectory, transcriptName), transcript.Meta{
			Name:      self.Name(),
			StartedAt: startedAt,
			Model:     self.meta.Model,
			Effort:    self.meta.Effort,
			Provider:  self.meta.Provider,
			Workspace: self.meta.WorkspaceDir,
		})
		if err != nil {
			self.transcriptLoggingEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		} else {
			self.transcriptRecorder = recorder
		}
	}
	if self.wireRecorder == nil && self.wireRecordingEnabled {
		recorder, err := wire.Open(filepath.Join(bundleDirectory, wireName), wire.Meta{
			Name:      self.Name(),
			StartedAt: startedAt,
			Model:     self.meta.Model,
			Effort:    self.meta.Effort,
			Provider:  self.meta.Provider,
			Workspace: self.meta.WorkspaceDir,
		}, self.queueWarning)
		if err != nil {
			self.wireRecordingEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("wire.http recording disabled: %w", err))
		} else {
			self.wireRecorder = recorder
		}
	}
}

func (self *Writer) queueWarning(err error) {
	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	self.warnings = append(self.warnings, err)
}

type writerObserver struct{ writer *Writer }

func (self writerObserver) Start(request req.Request) req.ExchangeObserver {
	if !self.writer.IsPersisted() {
		return nil
	}

	self.writer.startRecorders()
	self.writer.recorderMutex.Lock()
	recorder := self.writer.wireRecorder
	self.writer.recorderMutex.Unlock()
	if recorder == nil {
		return nil
	}
	return recorder.Start(request)
}

type Session struct {
	Name              string
	ID                string
	Meta              Meta
	StartedAt         time.Time
	TouchedAt         time.Time
	Events            []agent.Event
	Items             []json.RawMessage
	TurnCompletions   int
	Turns             []session.TurnSummary
	HasIncompleteTurn bool
	CacheReading      agent.CacheReading
}

func Read(directory string, name string) (*Session, error) {
	storedSession, err := session.Read(directory, name)
	if err != nil {
		return nil, err
	}
	return decode(storedSession)
}

func List(directory string) ([]*Session, error) {
	storedSessions, err := session.List(directory)
	if err != nil {
		return nil, err
	}

	out := make([]*Session, 0, len(storedSessions))
	for _, item := range storedSessions {
		decodedSession, err := decode(item)
		if err == nil {
			out = append(out, decodedSession)
		}
	}
	return out, nil
}

func decode(storedSession *session.Session) (*Session, error) {
	var meta Meta
	if len(storedSession.Meta) > 0 {
		if err := json.Unmarshal(storedSession.Meta, &meta); err != nil {
			return nil, err
		}
	}

	return &Session{
		Name:              storedSession.Name,
		ID:                storedSession.ID,
		Meta:              meta,
		StartedAt:         storedSession.StartedAt,
		TouchedAt:         storedSession.TouchedAt,
		Events:            storedSession.Events,
		Items:             storedSession.Items,
		TurnCompletions:   storedSession.TurnCompletions,
		Turns:             storedSession.Turns,
		HasIncompleteTurn: storedSession.HasIncompleteTurn,
		CacheReading:      storedSession.CacheReading,
	}, nil
}

func (self *Session) FirstMessage() string {
	for _, event := range self.Events {
		if event.Kind == agent.UserMessageEvent {
			return event.Text
		}
	}
	return ""
}

func (self *Session) Messages() int {
	count := 0
	for _, event := range self.Events {
		if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
			count++
		}
	}
	return count
}

func (self *Session) CanResume() bool {
	return !self.HasIncompleteTurn
}

func listingDataOf(storedSession *Session) (json.RawMessage, error) {
	meta := storedSession.Meta
	if isFast, isFound := model.LastRecordedFastMode(storedSession.Events); isFound {
		meta.IsFast = isFast
	}

	_, data, err := encodeMeta(meta)
	return data, err
}

func GetListingMeta(directory string, name string) (*session.Meta, error) {
	storedSession, err := Read(directory, name)
	if err != nil {
		return nil, err
	}
	data, err := listingDataOf(storedSession)
	if err != nil {
		return nil, err
	}
	return session.ReadMetaFromJournal(directory, name, data)
}

func RebuildMeta(directory string, name string) error {
	storedSession, err := Read(directory, name)
	if err != nil {
		return err
	}
	data, err := listingDataOf(storedSession)
	if err != nil {
		return err
	}
	return session.RebuildMeta(directory, name, data)
}

func RebuildMetaIfIdle(directory string, name string) (bool, error) {
	isRebuilt := false
	err := session.Unarchived(directory, name, func() error {
		heldLock, err := session.AcquireLock(directory, name)
		if errors.Is(err, session.ErrInUse) {
			return nil
		}
		if err != nil {
			return err
		}

		rebuildError := RebuildMeta(directory, name)
		releaseError := heldLock.Release()
		if err := errors.Join(rebuildError, releaseError); err != nil {
			return err
		}
		isRebuilt = true
		return nil
	})

	return isRebuilt, err
}

func StaleMeta(directory string) ([]string, error) {
	names, err := session.AllNames(directory)
	if err != nil {
		return nil, err
	}

	return StaleMetaOf(directory, names), nil
}

func StaleMetaOf(directory string, names []string) []string {
	var stale []string
	for _, name := range names {
		if _, err := session.ReadMeta(directory, name); err != nil {
			stale = append(stale, name)
		}
	}

	return stale
}

func RebuildStaleMeta(directory string) (int, error) {
	stale, err := StaleMeta(directory)
	if err != nil {
		return 0, err
	}

	for at, name := range stale {
		err := session.Unarchived(directory, name, func() error { return RebuildMeta(directory, name) })
		if err != nil {
			return at, fmt.Errorf("could not rebuild the listing metadata of %s: %w", name, err)
		}
	}

	return len(stale), nil
}

func Rebuild(directory string, name string) error {
	return session.Unarchived(directory, name, func() error {
		return rebuildTranscript(directory, name)
	})
}

func rebuildTranscript(directory string, name string) error {
	storedSession, err := Read(directory, name)
	if err != nil {
		return err
	}

	path := filepath.Join(directory, name, transcriptName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	recorder, err := transcript.Open(path, transcript.Meta{
		Name:      name,
		StartedAt: storedSession.StartedAt,
		Model:     storedSession.Meta.Model,
		Effort:    storedSession.Meta.Effort,
		Provider:  storedSession.Meta.Provider,
		Workspace: storedSession.Meta.WorkspaceDir,
	})
	if err != nil {
		return err
	}

	writeError := session.Records(directory, name, func(line session.Line) error {
		if line.Kind != session.Event || line.Event == nil {
			return nil
		}
		return recorder.Event(line.Time, *line.Event)
	})

	if closeError := recorder.Close(); writeError == nil {
		writeError = closeError
	}
	return writeError
}
