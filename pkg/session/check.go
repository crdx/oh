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

var ErrIncompleteJournalTail = errors.New("journal ends with an incomplete record")

type Inspection struct {
	Session           *Session
	Meta              *Meta
	MetaError         error
	IsArchived        bool
	IsInUse           bool
	HasIncompleteTail bool
	HasTranscript     bool
}

func Check(directory string, name string) error {
	inspection, err := Inspect(directory, name)
	if err != nil {
		return err
	}
	if inspection.IsInUse {
		return nil
	}
	if inspection.HasIncompleteTail {
		return fmt.Errorf("session %s: %w", name, ErrIncompleteJournalTail)
	}
	if inspection.MetaError != nil {
		return fmt.Errorf("listing metadata: %w", inspection.MetaError)
	}
	return nil
}

func Inspect(directory string, name string) (*Inspection, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	storedInfo, storedError := os.Lstat(Dir(directory, name))
	isStored := storedError == nil
	if storedError != nil && !errors.Is(storedError, fs.ErrNotExist) {
		return nil, storedError
	}

	archivePath := ArchivePath(directory, name)
	archiveInfo, archiveError := os.Lstat(archivePath)
	isArchived := archiveError == nil
	if archiveError != nil && !errors.Is(archiveError, fs.ErrNotExist) {
		return nil, archiveError
	}

	switch {
	case isStored && isArchived:
		return nil, errors.New("the session is both stored and archived")
	case !isStored && !isArchived:
		return nil, missing(directory, name)
	case isStored && !storedInfo.IsDir():
		return nil, errors.New("the stored session is not a directory")
	case isArchived && !archiveInfo.Mode().IsRegular():
		return nil, errors.New("the session archive is not a regular file")
	}

	if isArchived {
		inspection, err := inspectArchive(archivePath, name)
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		inspection.IsArchived = true
		return inspection, nil
	}

	heldLock, err := AcquireLock(directory, name)
	if errors.Is(err, ErrInUse) {
		return &Inspection{IsInUse: true}, nil
	}
	if err != nil {
		return nil, err
	}
	inspection, inspectError := inspectStored(directory, name)
	releaseError := heldLock.Release()
	if err := errors.Join(inspectError, releaseError); err != nil {
		return nil, err
	}
	return inspection, nil
}

func inspectStored(directory string, name string) (*Inspection, error) {
	if err := checkStoredFiles(directory, name); err != nil {
		return nil, err
	}

	journal, err := os.Open(journalPath(directory, name))
	if err != nil {
		return nil, err
	}
	storedSession, hasIncompleteTail, readError := inspectJournal(name, journal)
	closeError := journal.Close()
	if err := errors.Join(readError, closeError); err != nil {
		return nil, err
	}

	meta, metaError := readMetaPath(metaPath(directory, name), name)
	transcriptInfo, transcriptError := os.Lstat(filepath.Join(Dir(directory, name), "chat.md"))
	hasTranscript := transcriptError == nil && transcriptInfo.Mode().IsRegular()
	if transcriptError != nil && !errors.Is(transcriptError, fs.ErrNotExist) {
		return nil, transcriptError
	}

	return &Inspection{
		Session:           storedSession,
		Meta:              meta,
		MetaError:         metaError,
		HasIncompleteTail: hasIncompleteTail,
		HasTranscript:     hasTranscript,
	}, nil
}

func inspectJournal(name string, source io.Reader) (*Session, bool, error) {
	storedSession := &Session{Name: name}
	lineNumber := 0
	hasIncompleteTail, err := readRecords(name, source, true, func(line Line) error {
		lineNumber++
		if err := validateLine(name, line); err != nil {
			return fmt.Errorf("session %s: journal line %d: %w", name, lineNumber, err)
		}
		storedSession.take(line)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return storedSession, hasIncompleteTail, nil
}

func validateLine(name string, line Line) error {
	switch line.Kind {
	case Head:
		switch {
		case line.Name != name:
			return fmt.Errorf("head names session %q", line.Name)
		case line.ID == "":
			return errors.New("head has no ID")
		}
	case Event:
		if line.Event == nil {
			return errors.New("event has no payload")
		}
	case Item, TurnCompletion, TurnAbandonment:
	default:
		return fmt.Errorf("unknown record kind %q", line.Kind)
	}
	return nil
}

func readMetaPath(metaPath string, name string) (*Meta, error) {
	file, err := os.Open(metaPath) //nolint:gosec // a path built from a validated session name
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	encodedMeta, err := io.ReadAll(io.LimitReader(file, maxHeadLine+1))
	if err != nil {
		return nil, err
	}
	if len(encodedMeta) > maxHeadLine {
		return nil, errors.New("listing metadata is too large")
	}
	return decodeMeta(encodedMeta, name)
}

func checkStoredFiles(directory string, name string) error {
	info, err := os.Lstat(journalPath(directory, name))
	if err != nil {
		return fmt.Errorf("%s: %w", journalName, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", journalName)
	}

	info, err = os.Lstat(metaPath(directory, name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: %w", metaName, err)
	}
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", metaName)
	}
	return nil
}

func inspectArchive(archivePath string, name string) (*Inspection, error) {
	file, err := os.Open(archivePath) //nolint:gosec // a path built from a validated session name
	if err != nil {
		return nil, err
	}
	decompressor, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	inspection := &Inspection{}
	found := make(map[string]bool)
	archive := tar.NewReader(bufio.NewReader(decompressor))
	var inspectionError error
	for {
		header, nextError := archive.Next()
		if errors.Is(nextError, io.EOF) {
			break
		}
		if nextError != nil {
			inspectionError = errors.Join(inspectionError, nextError)
			break
		}

		relativeName, nameError := archiveRelativeName(header.Name, name)
		if nameError != nil {
			inspectionError = errors.Join(inspectionError, nameError)
			_, _ = io.Copy(io.Discard, archive) //nolint:gosec // complete archive verification must drain every entry
			continue
		}
		if found[relativeName] {
			inspectionError = errors.Join(inspectionError, fmt.Errorf("it holds %q more than once", header.Name))
			_, _ = io.Copy(io.Discard, archive) //nolint:gosec // complete archive verification must drain every entry
			continue
		}
		found[relativeName] = true

		switch header.Typeflag {
		case tar.TypeDir:
		case tar.TypeReg:
			switch relativeName {
			case journalName:
				storedSession, hasIncompleteTail, journalError := inspectJournal(name, archive)
				inspection.Session = storedSession
				inspection.HasIncompleteTail = hasIncompleteTail
				inspectionError = errors.Join(inspectionError, journalError)
			case metaName:
				encodedMeta, readError := io.ReadAll(io.LimitReader(archive, maxHeadLine+1))
				if readError == nil && len(encodedMeta) > maxHeadLine {
					readError = errors.New("listing metadata is too large")
				}
				if readError == nil {
					inspection.Meta, readError = decodeMeta(encodedMeta, name)
				}
				inspection.MetaError = readError
			case "chat.md":
				inspection.HasTranscript = true
			}
			if _, err := io.Copy(io.Discard, archive); err != nil { //nolint:gosec // complete archive verification must drain every entry
				inspectionError = errors.Join(inspectionError, err)
			}
		default:
			inspectionError = errors.Join(inspectionError, fmt.Errorf("it holds unsupported entry %q", header.Name))
		}
	}

	_, readError := io.Copy(io.Discard, decompressor) //nolint:gosec // reaching gzip's checksum verifies the complete local archive
	closeError := errors.Join(decompressor.Close(), file.Close())
	inspectionError = errors.Join(inspectionError, readError, closeError)
	if inspection.Session == nil && inspectionError == nil {
		inspectionError = fmt.Errorf("it holds no %q", path.Join(name, journalName))
	}
	if inspection.Meta == nil && inspection.MetaError == nil {
		inspection.MetaError = fmt.Errorf("it holds no %q", path.Join(name, metaName))
	}
	if inspectionError != nil {
		return nil, inspectionError
	}
	return inspection, nil
}

func archiveRelativeName(storedName string, name string) (string, error) {
	cleanedName := path.Clean(storedName)
	relativeName, isBelow := strings.CutPrefix(cleanedName, name+"/")
	if !isBelow || relativeName == "" || !fs.ValidPath(relativeName) {
		return "", fmt.Errorf("it holds %q, which is not part of the session", storedName)
	}
	return relativeName, nil
}
