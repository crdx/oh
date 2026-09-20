package drops

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"crdx.org/oh/internal/file"
)

type Keeper struct {
	files            *file.Root
	sessionDirectory string
	ensureSession    func() error

	saveMutex  sync.Mutex
	mountMutex sync.Mutex
	isMounted  bool
	close      func() error
}

func Open(files *file.Root, sessionDirectory string, ensureSession func() error) (*Keeper, error) {
	keeper := &Keeper{
		files:            files,
		sessionDirectory: sessionDirectory,
		ensureSession:    ensureSession,
		close:            func() error { return nil },
	}

	if err := keeper.mount(false); err != nil {
		return nil, fmt.Errorf("mount drops: %w", err)
	}

	return keeper, nil
}

func (self *Keeper) GetDirectory() string {
	return GetDirectory(self.sessionDirectory)
}

func (self *Keeper) SaveImage(mediaType string, data []byte) (string, error) {
	self.saveMutex.Lock()
	defer self.saveMutex.Unlock()

	path, err := SaveImage(self.sessionDirectory, self.ensureSession, mediaType, data)
	if err != nil {
		return "", err
	}
	if err := self.mount(true); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("make the pasted image readable: %w", err)
	}

	return path, nil
}

func (self *Keeper) SaveOutput(output string) (string, error) {
	self.saveMutex.Lock()
	defer self.saveMutex.Unlock()

	path, err := SaveOutput(self.sessionDirectory, self.ensureSession, output)
	if err != nil {
		return "", err
	}
	if err := self.mount(true); err != nil {
		return "", fmt.Errorf("make the saved output readable: %w", err)
	}

	return path, nil
}

func (self *Keeper) SaveHTML(contents []byte) (string, error) {
	self.saveMutex.Lock()
	defer self.saveMutex.Unlock()

	path, err := SaveHTML(self.sessionDirectory, self.ensureSession, contents)
	if err != nil {
		return "", err
	}
	if err := self.mount(true); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("make the saved HTML readable: %w", err)
	}

	return path, nil
}

func (self *Keeper) CopyFile(sourcePath string, fileName string) (string, error) {
	self.saveMutex.Lock()
	defer self.saveMutex.Unlock()

	path, err := CopyFile(self.sessionDirectory, self.ensureSession, sourcePath, fileName)
	if err != nil {
		return "", err
	}
	if err := self.mount(true); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("make the copied file readable: %w", err)
	}

	return path, nil
}

func (self *Keeper) Close() error {
	self.mountMutex.Lock()
	defer self.mountMutex.Unlock()

	return self.close()
}

func (self *Keeper) mount(shouldExist bool) error {
	self.mountMutex.Lock()
	defer self.mountMutex.Unlock()

	if self.isMounted {
		return nil
	}

	closeMount, isMounted, err := Mount(self.files, self.sessionDirectory)
	if err != nil {
		return err
	}
	if shouldExist && !isMounted {
		return errors.New("the drops directory disappeared")
	}
	if isMounted {
		self.close = closeMount
		self.isMounted = true
	}

	return nil
}
