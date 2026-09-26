package migrate

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

const (
	plainWireName      = "wire.http"
	compressedWireName = "wire.http.zst"
	wireWindowBytes    = 128 << 20
)

func compressWireTranscript(directory string, name string) error {
	bundle := filepath.Join(directory, name)
	plainPath := filepath.Join(bundle, plainWireName)
	compressedPath := filepath.Join(bundle, compressedWireName)

	plain, err := os.Open(plainPath) //nolint:gosec // a path built from a validated session name
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = plain.Close() }()

	if _, err := os.Lstat(compressedPath); err == nil {
		return fmt.Errorf("both %s and %s are present", plainWireName, compressedWireName)
	}

	compressedFile, err := os.CreateTemp(bundle, "wire-*.http.zst")
	if err != nil {
		return err
	}
	temporaryPath := compressedFile.Name()
	defer func() {
		_ = compressedFile.Close()
		_ = os.Remove(temporaryPath)
	}()

	plainDigest := sha256.New()
	if err := compress(compressedFile, io.TeeReader(plain, plainDigest)); err != nil {
		return fmt.Errorf("%s could not be compressed: %w", plainWireName, err)
	}
	if err := compressedFile.Sync(); err != nil {
		return err
	}

	if _, err := compressedFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decompressedDigest, err := digestOfDecompressed(compressedFile)
	if err != nil {
		return fmt.Errorf("%s could not be read back: %w", compressedWireName, err)
	}
	if !bytes.Equal(decompressedDigest.Sum(nil), plainDigest.Sum(nil)) {
		return fmt.Errorf("%s does not decompress to %s", compressedWireName, plainWireName)
	}

	if err := compressedFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, compressedPath); err != nil {
		return err
	}

	return os.Remove(plainPath)
}

func compress(into io.Writer, from io.Reader) error {
	encoder, err := zstd.NewWriter(
		into,
		zstd.WithWindowSize(wireWindowBytes),
		zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		return err
	}

	_, copyError := io.Copy(encoder, from)
	return errors.Join(copyError, encoder.Close())
}

func digestOfDecompressed(from io.Reader) (hash.Hash, error) {
	decoder, err := zstd.NewReader(
		from,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(wireWindowBytes),
		zstd.WithDecoderLowmem(false),
	)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, decoder); err != nil {
		return nil, err
	}

	return digest, nil
}
