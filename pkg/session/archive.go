package session

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	smallArchive = 64 << 20
	largeArchive = 512 << 20
)

const (
	ArchiveSuffix   = ".tgz"
	archiveFileMode = 0o600
	archiveDirMode  = 0o700
)

var (
	ErrArchived      = errors.New("the session is archived")
	ErrNotArchived   = errors.New("no archived session named")
	ErrAlreadyStored = errors.New("a session directory of that name is already stored")
)

func ArchivePath(directory string, name string) string {
	return filepath.Join(directory, name+ArchiveSuffix)
}

func IsArchived(directory string, name string) bool {
	if validateName(name) != nil {
		return false
	}

	archive, err := os.Stat(ArchivePath(directory, name))

	return err == nil && archive.Mode().IsRegular()
}

func ArchivedNames(directory string) ([]string, error) {
	found, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, candidate := range found {
		if candidate.IsDir() {
			continue
		}
		name, isArchive := strings.CutSuffix(candidate.Name(), ArchiveSuffix)
		if isArchive && validateName(name) == nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	return names, nil
}

func Archive(directory string, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if IsArchived(directory, name) {
		return fmt.Errorf("%w %q", ErrArchived, name)
	}

	heldLock, err := AcquireLock(directory, name)
	if err != nil {
		return err
	}
	defer func() { _ = heldLock.Release() }()

	temporaryPath, err := writeArchive(directory, name)
	if err != nil {
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
		return err
	}

	if err := os.Rename(temporaryPath, ArchivePath(directory, name)); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}

	if err := os.RemoveAll(Dir(directory, name)); err != nil {
		return err
	}

	return nil
}

func writeArchive(directory string, name string) (string, error) {
	file, err := os.CreateTemp(directory, name+"-*"+ArchiveSuffix)
	if err != nil {
		return "", err
	}
	temporaryPath := file.Name()

	if err := writeArchiveTo(file, Dir(directory, name), name); err != nil {
		_ = file.Close()
		return temporaryPath, err
	}
	if err := file.Close(); err != nil {
		return temporaryPath, err
	}
	if err := os.Chmod(temporaryPath, archiveFileMode); err != nil {
		return temporaryPath, err
	}

	return temporaryPath, nil
}

func writeArchiveTo(writer io.Writer, sessionDir string, name string) error {
	storedBytes, err := directorySize(sessionDir)
	if err != nil {
		return err
	}

	compressor, err := gzip.NewWriterLevel(writer, CompressionLevel(storedBytes))
	if err != nil {
		return err
	}
	archive := tar.NewWriter(compressor)

	walk := func(candidate string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relativePath, err := filepath.Rel(sessionDir, candidate)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}
		if !entry.Type().IsRegular() && !entry.IsDir() {
			return nil
		}

		return writeArchiveEntry(archive, candidate, path.Join(name, filepath.ToSlash(relativePath)), entry)
	}

	if err := filepath.WalkDir(sessionDir, walk); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}

	return compressor.Close()
}

func directorySize(sessionDir string) (int64, error) {
	var storedBytes int64

	err := filepath.WalkDir(sessionDir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		storedBytes += info.Size()

		return nil
	})

	return storedBytes, err
}

func CompressionLevel(storedBytes int64) int {
	switch {
	case storedBytes < smallArchive:
		return gzip.BestCompression
	case storedBytes < largeArchive:
		return gzip.DefaultCompression
	default:
		return gzip.BestSpeed
	}
}

func writeArchiveEntry(archive *tar.Writer, candidate string, storedName string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = storedName
	if entry.IsDir() {
		header.Name += "/"
	}

	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	if entry.IsDir() {
		return nil
	}

	file, err := os.Open(candidate) //nolint:gosec // a path walked below the session directory
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	_, err = io.Copy(archive, file)

	return err
}

func Delete(directory string, name string) error {
	if err := validateName(name); err != nil {
		return err
	}

	if IsArchived(directory, name) {
		return os.Remove(ArchivePath(directory, name))
	}

	heldLock, err := AcquireLock(directory, name)
	if err != nil {
		return err
	}
	defer func() { _ = heldLock.Release() }()

	return os.RemoveAll(Dir(directory, name))
}

func Restore(directory string, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if !IsArchived(directory, name) {
		return fmt.Errorf("%w %q", ErrNotArchived, name)
	}
	if _, err := os.Stat(Dir(directory, name)); err == nil {
		return fmt.Errorf("%w %q", ErrAlreadyStored, name)
	}

	unpackedDir, err := os.MkdirTemp(directory, name+"-*")
	if err != nil {
		return err
	}

	if err := unpackArchive(ArchivePath(directory, name), unpackedDir, name); err != nil {
		_ = os.RemoveAll(unpackedDir)
		return err
	}
	if err := os.Rename(unpackedDir, Dir(directory, name)); err != nil {
		_ = os.RemoveAll(unpackedDir)
		return err
	}

	return os.Remove(ArchivePath(directory, name))
}

func unpackArchive(archivePath string, unpackedDir string, name string) error {
	file, err := os.Open(archivePath) //nolint:gosec // a path built from a validated session name
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		unpackedPath, err := unpackedName(header.Name, unpackedDir, name)
		if err != nil {
			return err
		}
		if err := unpackEntry(archive, header, unpackedPath); err != nil {
			return err
		}
	}
}

func unpackedName(storedName string, unpackedDir string, name string) (string, error) {
	relativePath, isBelow := strings.CutPrefix(path.Clean(storedName), name+"/")
	if !isBelow || relativePath == "" {
		return "", fmt.Errorf("the archive holds %q, which is not part of the session", storedName)
	}

	unpackedPath := filepath.Join(unpackedDir, filepath.FromSlash(relativePath))
	if !strings.HasPrefix(unpackedPath, unpackedDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("the archive holds %q, which leaves the session directory", storedName)
	}

	return unpackedPath, nil
}

func unpackEntry(archive io.Reader, header *tar.Header, unpackedPath string) error {
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(unpackedPath, archiveDirMode)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(unpackedPath), archiveDirMode); err != nil {
			return err
		}
		return writeUnpackedFile(archive, unpackedPath)
	default:
		return nil
	}
}

func writeUnpackedFile(archive io.Reader, unpackedPath string) error {
	file, err := os.OpenFile(unpackedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, archiveFileMode) //nolint:gosec // a path checked to sit below the unpacked directory
	if err != nil {
		return err
	}

	if _, err := io.Copy(file, archive); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

func Unarchived(directory string, name string, change func() error) error {
	if err := validateName(name); err != nil {
		return err
	}
	if !IsArchived(directory, name) {
		return change()
	}

	if err := Restore(directory, name); err != nil {
		return err
	}

	changeError := change()
	if archiveError := Archive(directory, name); archiveError != nil {
		return errors.Join(changeError, archiveError)
	}

	return changeError
}

func ArchivedMeta(directory string, name string) (*Meta, error) {
	if !IsArchived(directory, name) {
		return nil, fmt.Errorf("%w %q", ErrNotArchived, name)
	}

	encodedMeta, err := readArchivedFile(ArchivePath(directory, name), path.Join(name, metaName))
	if err != nil {
		return nil, err
	}

	return decodeMeta(encodedMeta, name)
}

func readArchivedFile(archivePath string, storedName string) ([]byte, error) {
	archivedFile, err := openArchivedFile(archivePath, storedName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archivedFile.Close() }()

	return io.ReadAll(io.LimitReader(archivedFile, maxHeadLine))
}

type archivedFile struct {
	io.Reader

	file       *os.File
	decompress *gzip.Reader
}

func (self *archivedFile) Close() error {
	return errors.Join(self.decompress.Close(), self.file.Close())
}

func openArchivedFile(archivePath string, storedName string) (io.ReadCloser, error) {
	file, err := os.Open(archivePath) //nolint:gosec // a path built from a validated session name
	if err != nil {
		return nil, err
	}

	decompress, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	archive := tar.NewReader(decompress)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			err = fmt.Errorf("the archive holds no %q", storedName)
		}
		if err != nil {
			_ = decompress.Close()
			_ = file.Close()
			return nil, err
		}
		if path.Clean(header.Name) == storedName {
			return &archivedFile{Reader: archive, file: file, decompress: decompress}, nil
		}
	}
}
