package preview_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/preview"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/session"
)

const (
	fuzzedJournalBytes = 8192
	fuzzedRoom         = 80
)

func journalHead() string {
	return fmt.Sprintf(
		`{"kind":"head","version":%d,"meta":{"model":"a-model","workspaceDir":"","provider":"chat","effort":"medium","system_prompt":"You are a test assistant."}}`,
		session.JournalFormat,
	)
}

func wholeJournals() []string {
	head := journalHead()
	readRequest := func(path string) string {
		return `{"kind":"event","event":{"render":"` + path + `","emphasis":{"kind":"focus","value":"` + path + `"},"read_only":true,"kind":"tool_call_request","id":"call_1","name":"read","arguments":"{\"path\":\"` + path + `\"}"}}`
	}

	return []string{
		head + "\n" + `{"kind":"event","event":{"kind":"user_message","text":"hello"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"model_message","text":"# heading\n\n| a | b |\n|---|---|\n| 1 | 2 |"}}`,
		head + "\n" + readRequest("x") + "\n" +
			`{"kind":"event","event":{"kind":"tool_call_result","id":"call_1","name":"read","text":"one\ntwo","status":"success","stats":{"kind":"read","lines":2,"bytes":7}}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"model_reasoning","text":"thinking"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"mode_change","state":"rxwsnlg"}}` + "\n" +
			`{"kind":"event","event":{"kind":"mode_change","name":"w","state":"rxsnlg"}}`,
		head + "\n" + readRequest("x.png") + "\n" +
			`{"kind":"event","event":{"kind":"tool_call_result","id":"call_1","name":"read","text":"image/png, 68B","status":"success","stats":{"kind":"image","bytes":68},"picture":{"digest":"` + strings.Repeat("0", 64) + `","mediaType":"image/png","width":4,"height":4}}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"user_message","text":"\u001b[2J\u001b]52;c;cHduZWQ=\u0007"}}`,
	}
}

func damagedJournals() []string {
	head := journalHead()

	return []string{
		"",
		head,
		head + "\n" + `{"kind":"event"}`,
		head + "\n" + "{",
		head + "\n" + strings.Repeat("\n", 8),
	}
}

func previewJournal(t *testing.T, directory string, journal string) ([]string, error) {
	t.Helper()

	name := "fuzzed-session"
	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	return preview.Read(directory, name, work.At(directory), money.Dollar(), fuzzedRoom)
}

func TestEveryWholeSeedJournalIsDrawn(t *testing.T) {
	for _, journal := range wholeJournals() {
		rows, err := previewJournal(t, t.TempDir(), journal)
		if err != nil || len(rows) == 0 {
			t.Errorf("got %d rows and %v for the seed %q", len(rows), err, journal)
		}
	}
}

func FuzzADamagedJournalIsPreviewedWithoutFallingOver(fuzzer *testing.F) {
	for _, seed := range append(wholeJournals(), damagedJournals()...) {
		fuzzer.Add(seed)
	}

	directory := fuzzer.TempDir()

	fuzzer.Fuzz(func(t *testing.T, journal string) {
		if len(journal) > fuzzedJournalBytes {
			t.Skip("longer than a previewed journal is ever read")
		}

		rows, err := previewJournal(t, directory, journal)
		if err != nil {
			return
		}

		for _, row := range rows {
			for _, character := range row {
				if character == '\x1b' || character == '\r' {
					continue
				}
				if character < 0x20 || character == 0x7f {
					t.Fatalf("a previewed row holds %q: %q", character, row)
				}
			}
		}
	})
}
