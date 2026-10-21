package shell

import (
	"errors"
	"os"
)

type Roots struct {
	HomeDirectory    string
	Home             *os.Root
	ScratchDirectory string
	Scratch          *os.Root
}

func OpenRoots(homeDirectory string, scratchDirectory string) (Roots, error) {
	if err := os.MkdirAll(homeDirectory, 0o700); err != nil {
		return Roots{}, err
	}
	home, err := os.OpenRoot(homeDirectory)
	if err != nil {
		return Roots{}, err
	}
	scratch, err := os.OpenRoot(scratchDirectory)
	if err != nil {
		_ = home.Close()
		return Roots{}, err
	}
	return Roots{HomeDirectory: homeDirectory, Home: home, ScratchDirectory: scratchDirectory, Scratch: scratch}, nil
}

func (self Roots) Close() error {
	return errors.Join(self.Home.Close(), self.Scratch.Close())
}
