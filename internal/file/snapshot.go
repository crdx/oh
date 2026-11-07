package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const FileReadState = "file_read"

const hashChunkBytes = 1 << 20

var ErrNotRead = errors.New("read the file first")

var ErrChangedSinceRead = errors.New("file changed; re-read it")

type ReadSnapshot struct {
	Path string `json:"path"`
	Hash string `json:"sha256"`
}

type ReadState struct {
	Files []ReadSnapshot `json:"files"`
}

func NewReadSnapshot(path string, content []byte) ReadSnapshot {
	return ReadSnapshot{Path: path, Hash: Hash(content)}
}

func EncodeReadState(snapshots ...ReadSnapshot) json.RawMessage {
	state, _ := json.Marshal(ReadState{Files: snapshots}) //nolint:errchkjson // strings cannot fail JSON encoding
	return state
}

type Snapshots struct {
	mutex  sync.RWMutex
	hashes map[string]string
}

func NewSnapshots() *Snapshots {
	return &Snapshots{hashes: map[string]string{}}
}

func (self *Snapshots) Record(root *Root, name string, content []byte) string {
	hash := Hash(content)
	self.recordHash(root, name, hash)
	return hash
}

func (self *Snapshots) RestoreReadState(root *Root, payload json.RawMessage) error {
	var state ReadState
	if err := json.Unmarshal(payload, &state); err != nil {
		return err
	}

	for _, snapshot := range state.Files {
		resolvedRoot, name, err := root.Resolve(snapshot.Path)
		if errors.Is(err, ErrOutsideRoot) {
			continue
		}
		if err != nil {
			return err
		}
		if err := self.restoreHash(resolvedRoot, name, snapshot.Hash); err != nil {
			return err
		}
	}

	return nil
}

func (self *Snapshots) Check(root *Root, name string, content []byte) error {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	hash, ok := self.hashes[snapshotKey(root, name)]
	if !ok {
		return ErrNotRead
	}
	if hash != Hash(content) {
		return ErrChangedSinceRead
	}

	return nil
}

func (self *Snapshots) CheckFile(ctx context.Context, root *Root, name string) error {
	openedFile, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = openedFile.Close() }()

	info, err := openedFile.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return &fs.PathError{Op: "read", Path: name, Err: syscall.EISDIR}
	}

	self.mutex.RLock()
	recordedHash, isRecorded := self.hashes[snapshotKey(root, name)]
	self.mutex.RUnlock()
	if !isRecorded {
		return ErrNotRead
	}

	hash, err := hashOpenedFile(ctx, openedFile)
	if err != nil {
		return err
	}
	if hash != recordedHash {
		return ErrChangedSinceRead
	}

	return nil
}

func (self *Snapshots) restoreHash(root *Root, name string, hash string) error {
	decodedHash, err := hex.DecodeString(hash)
	if err != nil || len(decodedHash) != sha256.Size {
		return fmt.Errorf("invalid SHA-256 hash %q", hash)
	}

	self.recordHash(root, name, hex.EncodeToString(decodedHash))
	return nil
}

func (self *Snapshots) recordHash(root *Root, name string, hash string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.hashes[snapshotKey(root, name)] = hash
}

func snapshotKey(root *Root, name string) string {
	return filepath.Join(root.Name(), filepath.Clean(name))
}

func Hash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func HashFile(ctx context.Context, root *Root, name string) (string, error) {
	openedFile, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = openedFile.Close() }()

	return hashOpenedFile(ctx, openedFile)
}

func hashOpenedFile(ctx context.Context, openedFile *os.File) (string, error) {
	contentHash := sha256.New()
	chunk := make([]byte, hashChunkBytes)

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		count, err := openedFile.Read(chunk)
		_, _ = contentHash.Write(chunk[:count])
		if errors.Is(err, io.EOF) {
			return hex.EncodeToString(contentHash.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}
