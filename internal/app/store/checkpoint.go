package store

import (
	"errors"
	"fmt"

	"crdx.org/oh/pkg/session"
)

func (self *Writer) Checkpoint() (func() error, error) {
	if err := self.EnsurePersisted(); err != nil {
		return nil, err
	}
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	writer, isCanonical := self.innerWriter.(*session.Writer)
	if !isCanonical {
		return nil, errors.New("the session store has no checkpoints")
	}
	checkpoint, err := writer.Checkpoint()
	if err != nil {
		return nil, err
	}
	return func() error { return self.restoreCheckpoint(writer, checkpoint) }, nil
}

func (self *Writer) restoreCheckpoint(writer *session.Writer, checkpoint session.Checkpoint) error {
	self.eventMutex.Lock()
	defer self.eventMutex.Unlock()
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	if err := writer.RestoreCheckpoint(checkpoint); err != nil {
		return err
	}
	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	if err := writer.Sync(); err != nil {
		self.warnings = append(self.warnings, fmt.Errorf("sync journal after undo: %w", err))
	}
	if self.transcriptRecorder != nil {
		_ = self.transcriptRecorder.Close()
		self.transcriptRecorder = nil
	}
	if err := RebuildMeta(self.directory, self.Name()); err != nil {
		self.warnings = append(self.warnings, fmt.Errorf("rebuild listing after undo: %w", err))
	}
	if err := rebuildTranscript(self.directory, self.Name()); err != nil {
		self.transcriptLoggingEnabled = false
		self.warnings = append(self.warnings, fmt.Errorf("rebuild transcript after undo: %w", err))
	}
	return nil
}
