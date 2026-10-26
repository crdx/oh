package analyse

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCompressionRejectionsAreCountedBeforeCompletionAndInvalidateTheCache(t *testing.T) {
	directory := t.TempDir()
	name := "diagnostic-otter"
	prompt := event("00:00:01", `{"kind":"user_message","text":"hello"}`)
	firstRejection := event("00:00:02", `{"kind":"request_compression_rejected"}`)
	writeJournal(t, directory, name, prompt, firstRejection)
	cachePath := filepath.Join(t.TempDir(), "analysis.json")
	first, err := analyseWithCache(directory, cachePath, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	if first.Faults.Total.CompressionRejections != 1 || first.Sessions[0].Faults.CompressionRejections != 1 {
		t.Fatalf("the unfinished turn's rejection was not counted: %+v", first)
	}

	writeJournal(t, directory, name, prompt, firstRejection,
		event("00:00:03", `{"kind":"request_compression_rejected"}`),
		`{"kind":"turn_abandonment","time":"2026-09-01T00:00:04Z"}`,
	)
	second, err := analyseWithCache(directory, cachePath, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	if second.Faults.Total.CompressionRejections != 2 || second.Sessions[0].Faults.CompressionRejections != 2 {
		t.Fatalf("the changed journal or abandoned turn lost rejections: %+v", second)
	}
}

func TestCompressionRejectionsIncludeChildSessionsAndShowACompressionOnlyFault(t *testing.T) {
	analysis := aggregate([]SessionStatistics{
		{
			Name: "parent-otter", Provider: "codex", Model: "fake",
			Faults: FaultStatistics{Sessions: 1, CompressionRejections: 2},
			Subagents: []SessionStatistics{{
				Name: "child-ibis", Provider: "anthropic", Model: "fake",
				Faults: FaultStatistics{Sessions: 1, CompressionRejections: 3},
			}},
		},
		{
			Name: "other-wren", Provider: "anthropic", Model: "fake",
			Faults: FaultStatistics{Sessions: 1, CompressionRejections: 1},
		},
	}, nil)
	if analysis.Faults.Total.CompressionRejections != 6 || len(analysis.Faults.Providers) != 2 {
		t.Fatalf("provider or child rejection totals differ: %+v", analysis.Faults)
	}
	if analysis.Faults.Providers[0].CompressionRejections != 4 || analysis.Faults.Providers[1].CompressionRejections != 2 {
		t.Errorf("provider totals differ: %+v", analysis.Faults.Providers)
	}
	section := faultSection(analysis.Faults)
	column := slices.Index(section.header, "Compression Rejections")
	if column < 0 || len(section.rows) != 3 || section.rows[2].cells[column] != "6" {
		t.Fatalf("the compression-only fault is not visible: %+v", section)
	}
	if analysis.Faults.Total.Failures != 0 || analysis.Faults.Total.Retries != 0 {
		t.Error("compression rejections were also counted as model failures or retries")
	}
}

func TestOldJournalsHaveNoCompressionRejections(t *testing.T) {
	directory := t.TempDir()
	writeJournal(t, directory, "older-otter", event("00:00:01", `{"kind":"user_message","text":"hello"}`))
	statistics := analyseOne(t, directory, "older-otter")
	if statistics.Faults.CompressionRejections != 0 || !statistics.Faults.IsQuiet() {
		t.Fatalf("an old journal gained compression rejections: %+v", statistics.Faults)
	}
}
