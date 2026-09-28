package session

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func Check(directory string, name string) error {
	if err := validateName(name); err != nil {
		return err
	}

	storedInfo, storedError := os.Lstat(Dir(directory, name))
	isStored := storedError == nil
	if storedError != nil && !errors.Is(storedError, fs.ErrNotExist) {
		return storedError
	}

	archivePath := ArchivePath(directory, name)
	archiveInfo, archiveError := os.Lstat(archivePath)
	isArchived := archiveError == nil
	if archiveError != nil && !errors.Is(archiveError, fs.ErrNotExist) {
		return archiveError
	}

	switch {
	case isStored && isArchived:
		return errors.New("the session is both stored and archived")
	case !isStored && !isArchived:
		return missing(directory, name)
	case isStored && !storedInfo.IsDir():
		return errors.New("the stored session is not a directory")
	case isArchived && !archiveInfo.Mode().IsRegular():
		return errors.New("the session archive is not a regular file")
	}

	if isArchived {
		if err := checkArchive(archivePath, name); err != nil {
			return fmt.Errorf("archive: %w", err)
		}
	} else if err := checkStoredFiles(directory, name); err != nil {
		return err
	}

	if err := checkJournal(directory, name); err != nil {
		return err
	}
	if _, err := ReadMeta(directory, name); err != nil {
		return fmt.Errorf("listing metadata: %w", err)
	}

	return nil
}

func checkStoredFiles(directory string, name string) error {
	for _, fileName := range []string{journalName, metaName} {
		info, err := os.Lstat(filepath.Join(Dir(directory, name), fileName))
		if err != nil {
			return fmt.Errorf("%s: %w", fileName, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", fileName)
		}
	}

	return nil
}

func checkJournal(directory string, name string) error {
	lineNumber := 0
	return records(directory, name, false, func(line Line) error {
		lineNumber++

		var err error
		switch line.Kind {
		case Head:
			switch {
			case line.Name != name:
				err = fmt.Errorf("head names session %q", line.Name)
			case line.ID == "":
				err = errors.New("head has no ID")
			}
		case Event:
			if line.Event == nil {
				err = errors.New("event has no payload")
			}
		case Item, TurnCompletion:
		default:
			err = fmt.Errorf("unknown record kind %q", line.Kind)
		}

		if err != nil {
			return fmt.Errorf("session %s: journal line %d: %w", name, lineNumber, err)
		}
		return nil
	})
}

func checkArchive(archivePath string, name string) error {
	file, err := os.Open(archivePath) //nolint:gosec // the path is selected from the session store
	if err != nil {
		return err
	}
	decompressor, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return err
	}

	found := make(map[string]bool)
	hasJournal := false
	hasMeta := false
	archive := tar.NewReader(bufio.NewReader(decompressor))
	for {
		header, nextError := archive.Next()
		if errors.Is(nextError, io.EOF) {
			break
		}
		if nextError != nil {
			_ = decompressor.Close()
			_ = file.Close()
			return nextError
		}

		relativeName, err := archiveRelativeName(header.Name, name)
		if err != nil {
			_ = decompressor.Close()
			_ = file.Close()
			return err
		}
		if found[relativeName] {
			_ = decompressor.Close()
			_ = file.Close()
			return fmt.Errorf("it holds %q more than once", header.Name)
		}
		found[relativeName] = true

		switch header.Typeflag {
		case tar.TypeDir:
		case tar.TypeReg:
			if _, err := io.Copy(io.Discard, archive); err != nil { //nolint:gosec // checking the archive requires reading every streamed byte
				_ = decompressor.Close()
				_ = file.Close()
				return err
			}
			hasJournal = hasJournal || relativeName == journalName
			hasMeta = hasMeta || relativeName == metaName
		default:
			_ = decompressor.Close()
			_ = file.Close()
			return fmt.Errorf("it holds unsupported entry %q", header.Name)
		}
	}

	_, readError := io.Copy(io.Discard, decompressor) //nolint:gosec // reaching gzip's checksum verifies the complete local archive
	closeError := errors.Join(decompressor.Close(), file.Close())
	if err := errors.Join(readError, closeError); err != nil {
		return err
	}
	if !hasJournal {
		return fmt.Errorf("it holds no %q", path.Join(name, journalName))
	}
	if !hasMeta {
		return fmt.Errorf("it holds no %q", path.Join(name, metaName))
	}

	return nil
}

func archiveRelativeName(storedName string, name string) (string, error) {
	cleanedName := path.Clean(storedName)
	relativeName, isBelow := strings.CutPrefix(cleanedName, name+"/")
	if !isBelow || relativeName == "" || !fs.ValidPath(relativeName) {
		return "", fmt.Errorf("it holds %q, which is not part of the session", storedName)
	}

	return relativeName, nil
}
