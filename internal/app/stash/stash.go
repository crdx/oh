package stash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"crdx.org/oh/internal/format"
	"crdx.org/oh/internal/state"
)

const (
	FileName = "stash.json"
	version  = 1
)

type fileState struct {
	Version int      `json:"version"`
	Entries []string `json:"entries"`
}

type Store struct {
	path            string
	ensurePersisted func() error
	entries         []string
}

func Open(sessionDirectory string, ensurePersisted func() error) (*Store, error) {
	self := &Store{
		path:            filepath.Join(sessionDirectory, FileName),
		ensurePersisted: ensurePersisted,
	}

	if _, err := os.Stat(self.path); errors.Is(err, fs.ErrNotExist) {
		return self, nil
	} else if err != nil {
		return nil, fmt.Errorf("read stash: %w", err)
	}

	var record fileState
	if err := state.Read(self.path, version, &record); err != nil {
		return nil, fmt.Errorf("read stash: %w", err)
	}
	if err := format.Require(record.Version, version); err != nil {
		return nil, fmt.Errorf("read stash: %w", err)
	}
	self.entries = slices.Clone(record.Entries)

	return self, nil
}

func NewMemory() *Store {
	return &Store{}
}

func (self *Store) Count() int {
	return len(self.entries)
}

func (self *Store) Push(text string) (int, error) {
	entries := append(slices.Clone(self.entries), text)
	if err := self.save(entries); err != nil {
		return self.Count(), err
	}

	self.entries = entries
	return self.Count(), nil
}

func (self *Store) Exchange(current string) (string, int, bool, error) {
	if len(self.entries) == 0 {
		return "", 0, false, nil
	}

	last := len(self.entries) - 1
	restoredDraft := self.entries[last]
	entries := slices.Clone(self.entries[:last])
	if current != "" {
		entries = append(entries, current)
	}
	if err := self.save(entries); err != nil {
		return "", self.Count(), false, err
	}

	self.entries = entries
	return restoredDraft, self.Count(), true, nil
}

func (self *Store) save(entries []string) error {
	if self.path == "" {
		return nil
	}
	if self.ensurePersisted != nil {
		if err := self.ensurePersisted(); err != nil {
			return fmt.Errorf("persist session: %w", err)
		}
	}

	data, err := json.Marshal(fileState{Version: version, Entries: entries})
	if err != nil {
		return fmt.Errorf("encode stash: %w", err)
	}
	if err := state.WriteFile(self.path, data); err != nil {
		return fmt.Errorf("write stash: %w", err)
	}

	return nil
}
