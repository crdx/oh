package session

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

type Checkpoint struct {
	file *os.File
	size int64
	meta Meta
}

func (self *Writer) Checkpoint() (Checkpoint, error) {
	if err := self.ensureOpen(); err != nil {
		return Checkpoint{}, err
	}
	info, err := self.file.Stat()
	if err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{file: self.file, size: info.Size(), meta: self.listingMeta}, nil
}

func (self *Writer) RestoreCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.file == nil || checkpoint.file != self.file || self.lock == nil {
		return errors.New("the conversation checkpoint is no longer available")
	}
	reader, err := os.Open(journalPath(self.directory, self.name))
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	replacement, err := os.CreateTemp(Dir(self.directory, self.name), ".undo-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(replacement.Name()) }()
	if err := prepareCheckpointFile(replacement, reader, checkpoint.size); err != nil {
		_ = replacement.Close()
		return err
	}
	if err := os.Rename(replacement.Name(), filepath.Join(Dir(self.directory, self.name), journalName)); err != nil {
		_ = replacement.Close()
		return err
	}
	_ = self.file.Close()
	self.file = replacement
	self.lock.journalFile = replacement
	self.listingMeta = checkpoint.meta
	return nil
}

func prepareCheckpointFile(file *os.File, reader io.Reader, size int64) error {
	if _, err := io.CopyN(file, reader, size); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return lockFile(file)
}

func (self *Writer) Sync() error {
	if err := self.ensureOpen(); err != nil {
		return err
	}
	return errors.Join(self.file.Sync(), self.lock.sessionDir.Sync())
}
