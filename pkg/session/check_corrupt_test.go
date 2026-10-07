package session_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/session"
)

type archiveEntry struct {
	name string
	body []byte
	kind byte
}

func writeCheckedArchive(t *testing.T, directory string, name string, entries []archiveEntry) {
	t.Helper()

	file, err := os.Create(session.ArchivePath(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	compressor := gzip.NewWriter(file)
	archive := tar.NewWriter(compressor)
	for _, entry := range entries {
		if err := archive.WriteHeader(&tar.Header{
			Name:     entry.name,
			Mode:     0o600,
			Size:     int64(len(entry.body)),
			Typeflag: entry.kind,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTheHealthCheckRejectsMalformedArchiveEntries(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		makeEntries func(name string, journal []byte, meta []byte) []archiveEntry
		want        string
	}{
		{
			name: "foreign entry",
			makeEntries: func(name string, journal []byte, meta []byte) []archiveEntry {
				return []archiveEntry{
					{name: "elsewhere/session.jsonl", body: journal, kind: tar.TypeReg},
					{name: name + "/meta.json", body: meta, kind: tar.TypeReg},
				}
			},
			want: "not part of the session",
		},
		{
			name: "duplicate entry",
			makeEntries: func(name string, journal []byte, meta []byte) []archiveEntry {
				return []archiveEntry{
					{name: name + "/session.jsonl", body: journal, kind: tar.TypeReg},
					{name: name + "/session.jsonl", body: journal, kind: tar.TypeReg},
					{name: name + "/meta.json", body: meta, kind: tar.TypeReg},
				}
			},
			want: "more than once",
		},
		{
			name: "symbolic link",
			makeEntries: func(name string, journal []byte, meta []byte) []archiveEntry {
				return []archiveEntry{
					{name: name + "/session.jsonl", body: journal, kind: tar.TypeReg},
					{name: name + "/meta.json", body: meta, kind: tar.TypeReg},
					{name: name + "/link", kind: tar.TypeSymlink},
				}
			},
			want: "unsupported entry",
		},
		{
			name: "no journal",
			makeEntries: func(name string, journal []byte, meta []byte) []archiveEntry {
				return []archiveEntry{{name: name + "/meta.json", body: meta, kind: tar.TypeReg}}
			},
			want: "session.jsonl\"",
		},
		{
			name: "no metadata",
			makeEntries: func(name string, journal []byte, meta []byte) []archiveEntry {
				return []archiveEntry{{name: name + "/session.jsonl", body: journal, kind: tar.TypeReg}}
			},
			want: "meta.json\"",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			name := checkedSession(t, directory)
			journal, err := os.ReadFile(filepath.Join(directory, name, "session.jsonl")) //nolint:gosec // the test's own session
			if err != nil {
				t.Fatal(err)
			}
			meta, err := os.ReadFile(filepath.Join(directory, name, "meta.json")) //nolint:gosec // the test's own session
			if err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(session.Dir(directory, name)); err != nil {
				t.Fatal(err)
			}
			writeCheckedArchive(t, directory, name, testCase.makeEntries(name, journal, meta))

			if err := session.Check(directory, name); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("got %v, want an archive error containing %q", err, testCase.want)
			}
		})
	}
}
