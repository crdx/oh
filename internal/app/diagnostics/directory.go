package diagnostics

import (
	"os"
	"path/filepath"
	"time"
)

const (
	DirectoryName      = "debug"
	StallDirectoryName = "stalls"
	CPUDirectoryName   = "cpu"
	fileTimeFormat     = "20060102-150405.000000000"
)

func writeEntry(sessionDirectory string, kind string, at time.Time, extension string, contents []byte) {
	session, err := os.OpenRoot(sessionDirectory)
	if err != nil {
		return
	}
	defer func() { _ = session.Close() }()

	directory := filepath.Join(DirectoryName, kind)
	if err := session.MkdirAll(directory, 0o700); err != nil {
		return
	}

	name := filepath.Join(directory, at.Format(fileTimeFormat)+extension)
	_ = session.WriteFile(name, contents, 0o600)
}
