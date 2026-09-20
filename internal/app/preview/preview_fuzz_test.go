package preview_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/preview"
	"crdx.org/io/internal/app/work"
)

const (
	fuzzedJournalBytes = 8192
	fuzzedRoom         = 80
)

func FuzzADamagedJournalIsPreviewedWithoutFallingOver(fuzzer *testing.F) {
	head := `{"kind":"head","version":13,"meta":{"model":"a-model","workspaceDir":"","provider":"chat","effort":"medium","system_prompt":"You are a test assistant."}}`

	for _, seed := range []string{
		"",
		head,
		head + "\n" + `{"kind":"event","event":{"kind":"user_message","text":"hello"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"model_message","text":"# heading\n\n| a | b |\n|---|---|\n| 1 | 2 |"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"tool_call","name":"read","arguments":"{\"path\":\"x\"}"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"reasoning","text":"thinking"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"mode_change","state":"rxwngl"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"picture_drawn","text":"/nowhere.png"}}`,
		head + "\n" + `{"kind":"event","event":{"kind":"user_message","text":"\u001b[2J\u001b]52;c;cHduZWQ=\u0007"}}`,
		head + "\n" + `{"kind":"event"}`,
		head + "\n" + "{",
		head + "\n" + strings.Repeat("\n", 8),
	} {
		fuzzer.Add(seed)
	}

	directory := fuzzer.TempDir()

	fuzzer.Fuzz(func(t *testing.T, journal string) {
		if len(journal) > fuzzedJournalBytes {
			t.Skip("longer than a previewed journal is ever read")
		}

		name := "fuzzed-session"
		if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(journal), 0o600); err != nil {
			t.Fatal(err)
		}

		rows, err := preview.Read(directory, name, work.At(directory), fuzzedRoom)
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
