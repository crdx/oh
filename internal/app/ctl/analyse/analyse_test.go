package analyse

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/markdown"
	"crdx.org/io/internal/app/model"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/money"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

const journalProvider = "codex"

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestZeroCachedTokensAreAMiss(t *testing.T) {
	statistics := CacheStatistics{}
	statistics.record(usageReport{inputTokens: 9000})
	statistics.record(usageReport{inputTokens: 12000, cachedTokens: 8000})

	if statistics.Requests != 2 || statistics.Hits != 1 || statistics.Misses != 1 {
		t.Errorf("got requests=%d hits=%d misses=%d", statistics.Requests, statistics.Hits, statistics.Misses)
	}
}

func TestHistoricalAnalysisCacheIsReusedAndInvalidated(t *testing.T) {
	directory := t.TempDir()
	name := "cached-otter"
	firstRequest := event("00:00:01",
		`{"kind":"model_message","usage":{"input_tokens":9000,"output_tokens":120,"cache":{"read_tokens":8000}}}`)
	writeJournal(t, directory, name, firstRequest)
	cachePath := filepath.Join(t.TempDir(), "analysis.json")

	first, err := analyseWithCache(directory, cachePath, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	readCache := readAnalysisCache(cachePath)
	if len(readCache.Sessions) != 1 || first.PromptCache.Total.Requests != 1 {
		t.Fatalf("cache or first analysis was incomplete: %#v %#v", readCache, first)
	}

	writeJournal(t, directory, name, firstRequest, event("00:00:02",
		`{"kind":"model_message","usage":{"input_tokens":10000,"output_tokens":60,"cache":{"read_tokens":0}}}`))
	second, err := analyseWithCache(directory, cachePath, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	if second.PromptCache.Total.Requests != 2 || second.PromptCache.Total.Misses != 1 {
		t.Errorf("a changed journal did not invalidate the cache: %#v", second)
	}
}

func TestOutputTokensArrivingAfterTheirRequestAreCounted(t *testing.T) {
	directory := t.TempDir()
	writeJournal(t, directory, "thoughtful-ibis",
		event("00:00:01",
			`{"kind":"model_reasoning","usage":{"input_tokens":11417,"cache":{"read_tokens":10839,`+
				`"write_tokens":576}}}`),
		event("00:00:02", `{"kind":"tool_call_request","name":"bash","id":"1","usage":{"output_tokens":567}}`),
	)

	statistics := analyseOne(t, directory, "thoughtful-ibis")

	if statistics.Cache.Requests != 1 {
		t.Errorf("got %d requests, want 1", statistics.Cache.Requests)
	}
	if statistics.Cache.OutputTokens != 567 {
		t.Errorf("got %d output tokens, want 567", statistics.Cache.OutputTokens)
	}
}

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "oh"))
}

func TestAJournalReportsTurnsToolsAndFaults(t *testing.T) {
	directory := t.TempDir()
	writeJournal(t, directory, "busy-gannet",
		event("00:00:02", `{"kind":"user_message","text":"hello"}`),
		event("00:00:03", `{"kind":"model_reasoning","text":"thinking"}`),
		event("00:00:04", `{"kind":"tool_call_request","name":"bash","id":"1"}`),
		event("00:00:06", `{"kind":"tool_call_result","name":"bash","id":"1","status":"error","took":2000000000}`),
		event("00:00:07", `{"kind":"tool_call_request","name":"read","id":"2"}`),
		event("00:00:08", `{"kind":"tool_call_result","name":"read","id":"2","status":"success","took":500000000}`),
		event("00:00:09", `{"kind":"request_retry","attempt":2}`),
		event("00:00:10", `{"kind":"turn_interruption"}`),
		event("00:00:11", `{"kind":"silent_turn"}`),
		event("00:00:12", `{"kind":"prefix_rewrite","text":"tools"}`),
		event("00:00:13", `{"kind":"cache_rebuild","name":"expired","usage":{"cache":{"write_tokens":4000}}}`),
		event("00:00:14",
			`{"kind":"model_message","text":"done","usage":{"input_tokens":9000,"output_tokens":700,`+
				`"cache":{"read_tokens":8000,"write_tokens":400}}}`),
		`{"kind":"turn_completion","time":"2026-09-01T00:00:15Z","turn":{"took":13000000000,"input_tokens":9000}}`,
	)

	statistics := analyseOne(t, directory, "busy-gannet")

	if statistics.Provider != journalProvider {
		t.Errorf("got provider %q, want %q", statistics.Provider, journalProvider)
	}
	activity := statistics.Activity
	if activity.Turns != 1 || activity.Prompts != 1 || activity.Replies != 1 ||
		activity.ReasoningBlocks != 1 || activity.ToolCalls != 2 {
		t.Errorf("got activity %#v", activity)
	}
	if activity.TurnTime != 13*time.Second || activity.LongestTurn != 13*time.Second {
		t.Errorf("got turn time %s and longest %s", activity.TurnTime, activity.LongestTurn)
	}
	if activity.SessionTime != 15*time.Second {
		t.Errorf("got session time %s, want 15s", activity.SessionTime)
	}

	faults := statistics.Faults
	if faults.Retries != 1 || faults.Interruptions != 1 || faults.SilentTurns != 1 || faults.PrefixRewrites != 1 {
		t.Errorf("got faults %#v", faults)
	}
	if faults.CacheRebuilds.Expiries != 1 || faults.CacheRebuilds.Count() != 1 ||
		faults.CacheRebuilds.WrittenTokens != 4000 {
		t.Errorf("got rebuilds %#v", faults.CacheRebuilds)
	}

	want := []ToolStatistics{
		{Name: "bash", Calls: 1, Failures: 1, Took: 2 * time.Second},
		{Name: "read", Calls: 1, Took: 500 * time.Millisecond},
	}
	if !reflect.DeepEqual(statistics.Tools, want) {
		t.Errorf("got tools %#v, want %#v", statistics.Tools, want)
	}
	if statistics.Cache.OutputTokens != 700 {
		t.Errorf("got %d output tokens, want 700", statistics.Cache.OutputTokens)
	}
}

func TestEverySessionTheJournalCannotAnswerForIsSkipped(t *testing.T) {
	const wholeRequest = `{"kind":"model_message","usage":{"input_tokens":9000,"output_tokens":120,` +
		`"cache":{"read_tokens":8000}}}`

	journals := map[string][]string{
		"whole-heron": {
			event("00:00:01", wholeRequest),
			`{"kind":"turn_completion","time":"2026-09-01T00:00:05Z","turn":{"took":4000000000}}`,
		},
		"untimed-crane": {
			event("00:00:01", wholeRequest),
			`{"kind":"turn_completion","time":"2026-09-01T00:00:05Z"}`,
		},
		"outputless-marmot": {
			event("00:00:01", `{"kind":"model_message","usage":{"input_tokens":9000,"cache":{"read_tokens":8000}}}`),
		},
		"cacheless-vole": {
			event("00:00:01", `{"kind":"model_message","usage":{"input_tokens":9000,"output_tokens":120}}`),
		},
		"silent-swift": {
			event("00:00:01", `{"kind":"model_message","text":"hello"}`),
		},
	}

	directory := t.TempDir()
	names := make([]string, 0, len(journals))
	for name, events := range journals {
		writeJournal(t, directory, name, events...)
		names = append(names, name)
	}
	slices.Sort(names)

	sessions, skippedCount, err := readSessions(directory, "", names)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 || sessions[0].Name != "whole-heron" {
		t.Fatalf("got sessions %#v, want only whole-heron", sessions)
	}
	if skippedCount != len(journals)-1 {
		t.Errorf("got %d skipped, want %d", skippedCount, len(journals)-1)
	}
}

func TestAnAverageTurnCoversEveryTurnThatWasRead(t *testing.T) {
	directory := t.TempDir()
	writeJournal(t, directory, "hasty-crane",
		event("00:00:01",
			`{"kind":"model_message","usage":{"input_tokens":9000,"output_tokens":120,"cache":{"read_tokens":8000}}}`),
		`{"kind":"turn_completion","time":"2026-09-01T00:00:05Z","turn":{"took":2000000000}}`,
		`{"kind":"turn_completion","time":"2026-09-01T00:00:09Z","turn":{"took":4000000000}}`,
	)

	activity := analyseOne(t, directory, "hasty-crane").Activity

	if activity.Turns != 2 {
		t.Errorf("got %d turns, want 2", activity.Turns)
	}
	if activity.AverageTurn() != 3*time.Second {
		t.Errorf("got an average turn of %s, want 3s", activity.AverageTurn())
	}
}

func TestARebuiltCacheIsNotCountedAsUsage(t *testing.T) {
	directory := t.TempDir()
	writeJournal(t, directory, "rebuilt-heron",
		event("00:00:02", `{"kind":"cache_rebuild","name":"reopened","usage":{"input_tokens":50,"cache":{"write_tokens":9000}}}`),
		event("00:00:03",
			`{"kind":"model_message","usage":{"input_tokens":9000,"output_tokens":140,`+
				`"cache":{"read_tokens":0,"write_tokens":9000}}}`),
	)

	statistics := analyseOne(t, directory, "rebuilt-heron")

	if statistics.Cache.Requests != 1 {
		t.Errorf("got %d requests, want 1", statistics.Cache.Requests)
	}
	if statistics.Faults.CacheRebuilds.Reopenings != 1 || statistics.Faults.CacheRebuilds.WrittenTokens != 9000 {
		t.Errorf("got rebuilds %#v", statistics.Faults.CacheRebuilds)
	}
}

func TestSpendIsChargedFromTheListedPrices(t *testing.T) {
	analysis := aggregate([]SessionStatistics{{
		Name:     "thrifty-vole",
		Provider: "codex",
		Model:    "gpt-5.6-sol",
		Cache: CacheStatistics{
			Sessions:      1,
			Requests:      1,
			InputTokens:   1_000_000,
			CachedTokens:  600_000,
			WrittenTokens: 0,
			OutputTokens:  1_000_000,
		},
	}}, goldenPricebook())

	const want = 0.4*1.25 + 0.6*0.125 + 10
	if spend := analysis.Models.Total.Spend; math.Abs(spend-want) > 0.000001 {
		t.Errorf("got spend %f, want %f", spend, want)
	}
	if analysis.Models.UnpricedModels != 0 {
		t.Errorf("got %d unpriced models, want none", analysis.Models.UnpricedModels)
	}
}

func TestAnUnknownModelHasNoSpend(t *testing.T) {
	analysis := aggregate([]SessionStatistics{{
		Name:     "mystery-shrew",
		Provider: "openrouter",
		Model:    "kimi-k2-thinking",
		Cache:    CacheStatistics{Sessions: 1, Requests: 1, InputTokens: 1000},
	}}, goldenPricebook())

	if analysis.Models.UnpricedModels != 1 || analysis.Models.Total.IsPriced {
		t.Errorf("got models %#v", analysis.Models)
	}
}

func TestASectionTitleIsDrawnLikeAMarkdownHeading(t *testing.T) {
	const title = "Prompt cache"

	rows := markdown.Render("# "+title, 80)
	drawn := style.MarkdownHeading(title)

	if len(rows) != 1 || rows[0] != drawn {
		t.Errorf("markdown drew %q, and a section title draws %q", rows, drawn)
	}
}

func TestGoldenTheWholeAnalysisMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	if err := writeText(goldenAnalysis(), presentation{currency: money.Dollar()}, &output); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "report.txt", output.String())
}

func TestGoldenTheColouredAnalysisMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	shown := presentation{currency: money.Dollar(), isPerSession: true}
	if err := drawText(goldenAnalysis(), shown, &output); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "report.ansi", strutil.VisibleEscapes(output.String()))
}

func TestGoldenOneOfEverythingNeedsNoTotalsAndDrawsNoFaults(t *testing.T) {
	sessions := []SessionStatistics{{
		Name:      "lone-marten",
		Provider:  "codex",
		Model:     "gpt-5.6-sol",
		StartedAt: time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, time.September, 1, 9, 4, 0, 0, time.UTC),
		Cache: CacheStatistics{
			Sessions:        1,
			Requests:        2,
			Hits:            2,
			InputTokens:     40_000,
			CachedTokens:    30_000,
			OutputTokens:    1_200,
			PeakInputTokens: 25_000,
		},
		Activity: ActivityStatistics{
			Sessions:    1,
			Turns:       2,
			Prompts:     2,
			Replies:     2,
			ToolCalls:   3,
			TurnTime:    50 * time.Second,
			LongestTurn: 30 * time.Second,
			SessionTime: 4 * time.Minute,
		},
		Faults: FaultStatistics{Sessions: 1},
		Tools:  []ToolStatistics{{Name: "read", Calls: 3, Took: 300 * time.Millisecond}},
	}}

	var output bytes.Buffer
	shown := presentation{currency: money.Dollar(), isPerSession: true}
	if err := writeText(aggregate(sessions, goldenPricebook()), shown, &output); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(output.String(), "Faults") {
		t.Error("a quiet analysis should draw no faults")
	}
	for line := range strings.SplitSeq(output.String(), "\n") {
		if strings.HasPrefix(line, totalName) {
			t.Errorf("a single row of each kind should need no total, got %q", line)
		}
	}
	assertGolden(t, "one-of-everything.txt", output.String())
}

func TestGoldenSeveralUnpricedModelsAreCountedTogether(t *testing.T) {
	sessions := []SessionStatistics{
		{
			Name:     "first-vole",
			Provider: "openrouter",
			Model:    "kimi-k2-thinking",
			Cache:    CacheStatistics{Sessions: 1, Requests: 2, Misses: 2, InputTokens: 4000, OutputTokens: 300},
			Activity: ActivityStatistics{
				Sessions:    1,
				Turns:       1,
				Prompts:     1,
				TurnTime:    30 * time.Second,
				LongestTurn: 30 * time.Second,
			},
		},
		{
			Name:     "second-vole",
			Provider: "ollama",
			Model:    "qwen3.8:27b",
			Cache:    CacheStatistics{Sessions: 1, Requests: 3, Misses: 3, InputTokens: 9000, OutputTokens: 600},
			Activity: ActivityStatistics{
				Sessions:    1,
				Turns:       2,
				Prompts:     2,
				TurnTime:    50 * time.Second,
				LongestTurn: 40 * time.Second,
			},
		},
		{
			Name:     "third-vole",
			Provider: "codex",
			Model:    "gpt-5.6-sol",
			Cache:    CacheStatistics{Sessions: 1, Requests: 3, Hits: 3, InputTokens: 30_000, CachedTokens: 24_000, OutputTokens: 900},
			Activity: ActivityStatistics{
				Sessions:    1,
				Turns:       3,
				Prompts:     3,
				TurnTime:    90 * time.Second,
				LongestTurn: 45 * time.Second,
			},
		},
	}

	var output bytes.Buffer
	shown := presentation{currency: money.Dollar()}
	if err := writeText(aggregate(sessions, goldenPricebook()), shown, &output); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "unpriced-models.txt", output.String())
}

func TestGoldenNamedSessionsAreDrawnRowByRowInTheChosenCurrency(t *testing.T) {
	var output bytes.Buffer
	shown := presentation{currency: money.In("GBP", 0.8), isPerSession: true}
	if err := writeText(goldenAnalysis(), shown, &output); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "sessions.txt", output.String())
}

func TestGoldenEveryReportWithoutATotalMatchesTheGolden(t *testing.T) {
	reports := []struct {
		name     string
		analysis Analysis
	}{
		{
			name:     "nothing was recorded",
			analysis: Analysis{},
		},
		{
			name:     "nothing was read, and every session was skipped",
			analysis: Analysis{SkippedSessions: 496},
		},
		{
			name: "one session skipped beside one unpriced model",
			analysis: Analysis{
				Models: ModelAnalysis{
					Models: []ModelStatistics{{
						Provider:     "ollama",
						Model:        "qwen3.8:27b",
						Sessions:     1,
						Requests:     3,
						InputTokens:  9000,
						OutputTokens: 600,
					}},
					UnpricedModels: 1,
				},
				SkippedSessions: 1,
			},
		},
		{
			name: "one provider, which is its own total",
			analysis: Analysis{PromptCache: PromptCacheAnalysis{
				Providers: []CacheStatistics{{
					Provider:        "codex",
					Sessions:        1,
					Requests:        2,
					Hits:            1,
					Misses:          1,
					InputTokens:     12000,
					CachedTokens:    8000,
					WrittenTokens:   1000,
					OutputTokens:    3000,
					PeakInputTokens: 9000,
				}},
				Total: CacheStatistics{
					Sessions:        1,
					Requests:        2,
					Hits:            1,
					Misses:          1,
					InputTokens:     12000,
					CachedTokens:    8000,
					WrittenTokens:   1000,
					OutputTokens:    3000,
					PeakInputTokens: 9000,
				},
			}},
		},
		{
			name: "counts in their thousands, tokens in their billions, and nothing written",
			analysis: Analysis{
				PromptCache: PromptCacheAnalysis{
					Providers: []CacheStatistics{{
						Provider:        "codex",
						Sessions:        186,
						Requests:        22138,
						Hits:            21803,
						Misses:          335,
						InputTokens:     3_810_000_000,
						CachedTokens:    3_750_000_000,
						WrittenTokens:   0,
						OutputTokens:    12_400_000,
						PeakInputTokens: 614_000,
					}},
				},
				Activity: ActivityAnalysis{
					Providers: []ActivityStatistics{{
						Provider:        "codex",
						Sessions:        186,
						Turns:           4102,
						Prompts:         4102,
						Replies:         3980,
						ReasoningBlocks: 51_400,
						ToolCalls:       88_310,
						TurnTime:        740 * time.Hour,
						LongestTurn:     52 * time.Minute,
						SessionTime:     2100 * time.Hour,
					}},
				},
			},
		},
	}

	var drawn strings.Builder
	for _, report := range reports {
		fmt.Fprintf(&drawn, "=== %s ===\n", report.name)
		if err := writeText(report.analysis, presentation{currency: money.Dollar()}, &drawn); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&drawn)
	}

	assertGolden(t, "sparse.txt", drawn.String())
}

func TestGoldenTheJSONAnalysisMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	if err := writeJSON(goldenAnalysis(), &output); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "analysis.json", output.String())
}

func TestGoldenTheJSONOfAnEmptyAnalysisMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	if err := writeJSON(aggregate(nil, pricebook{}), &output); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "empty.json", output.String())
}

func TestArchivedSessionsAreNotAnalysed(t *testing.T) {
	directory := t.TempDir()
	writeJournal(t, directory, "quiet-otter")
	writeJournal(t, directory, "packed-otter")
	if err := session.Archive(directory, "packed-otter"); err != nil {
		t.Fatal(err)
	}

	names, err := selectNames(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"quiet-otter"}) {
		t.Errorf("got names %#v, want only the stored session", names)
	}

	if _, err := selectNames(directory, []string{"packed-otter"}); err == nil ||
		!strings.Contains(err.Error(), "archived") {
		t.Errorf("got error %v, want an archived session refusal", err)
	}
}

func goldenAnalysis() Analysis {
	analysis := aggregate(goldenSessions(), goldenPricebook())
	analysis.SkippedSessions = 12

	return analysis
}

func goldenPricebook() pricebook {
	return pricebook{
		"anthropic/claude-sonnet-4-6": {Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
		"codex/gpt-5.6-sol":           {Input: 1.25, Output: 10, CacheRead: 0.125},
	}
}

func goldenSessions() []SessionStatistics {
	startedAt := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)

	return []SessionStatistics{
		{
			Name:      "amber-wolf",
			Provider:  "anthropic",
			Model:     "claude-sonnet-4-6",
			Effort:    "medium",
			StartedAt: startedAt,
			EndedAt:   startedAt.Add(42 * time.Minute),
			Cache: CacheStatistics{
				Sessions:        1,
				Requests:        5,
				Hits:            4,
				Misses:          1,
				InputTokens:     300_000,
				CachedTokens:    270_000,
				WrittenTokens:   15_000,
				OutputTokens:    9_000,
				PeakInputTokens: 100_000,
			},
			Activity: ActivityStatistics{
				Sessions:        1,
				Turns:           5,
				Prompts:         5,
				Replies:         5,
				ReasoningBlocks: 12,
				ToolCalls:       31,
				TurnTime:        6 * time.Minute,
				LongestTurn:     2 * time.Minute,
				SessionTime:     42 * time.Minute,
			},
			Faults: FaultStatistics{
				Sessions:      1,
				Retries:       1,
				CacheRebuilds: RebuildStatistics{Expiries: 1, WrittenTokens: 15_000},
			},
			Tools: []ToolStatistics{
				{Name: "bash", Calls: 12, Failures: 2, Took: 40 * time.Second},
				{Name: "read", Calls: 19, Took: 3 * time.Second},
			},
		},
		{
			Name:      "quiet-otter",
			Provider:  "anthropic",
			Model:     "claude-sonnet-4-6",
			Effort:    "medium",
			StartedAt: startedAt.Add(time.Hour),
			EndedAt:   startedAt.Add(time.Hour + 8*time.Minute),
			Cache: CacheStatistics{
				Sessions:        1,
				Requests:        3,
				Hits:            2,
				Misses:          1,
				InputTokens:     200_000,
				CachedTokens:    180_000,
				WrittenTokens:   10_000,
				OutputTokens:    4_000,
				PeakInputTokens: 90_000,
			},
			Activity: ActivityStatistics{
				Sessions:    1,
				Turns:       3,
				Prompts:     3,
				Replies:     3,
				ToolCalls:   4,
				TurnTime:    90 * time.Second,
				LongestTurn: 50 * time.Second,
				SessionTime: 8 * time.Minute,
			},
			Faults: FaultStatistics{Sessions: 1, Interruptions: 2, SilentTurns: 1},
			Tools:  []ToolStatistics{{Name: "read", Calls: 4, Took: time.Second}},
		},
		{
			Name:      "rustic-giraffe",
			Provider:  "codex",
			Model:     "gpt-5.6-sol",
			Effort:    "medium",
			StartedAt: startedAt.Add(2 * time.Hour),
			EndedAt:   startedAt.Add(2*time.Hour + 95*time.Minute),
			Cache: CacheStatistics{
				Sessions:        1,
				Requests:        10,
				Hits:            7,
				Misses:          3,
				InputTokens:     250_000,
				CachedTokens:    175_000,
				WrittenTokens:   10_000,
				OutputTokens:    22_000,
				PeakInputTokens: 40_000,
			},
			Activity: ActivityStatistics{
				Sessions:        1,
				Turns:           10,
				Prompts:         11,
				Replies:         9,
				ReasoningBlocks: 44,
				ToolCalls:       120,
				TurnTime:        25 * time.Minute,
				LongestTurn:     5 * time.Minute,
				SessionTime:     95 * time.Minute,
			},
			Faults: FaultStatistics{
				Sessions:       1,
				Retries:        3,
				Failures:       1,
				PrefixRewrites: 2,
				CacheRebuilds:  RebuildStatistics{Reopenings: 1, Settlements: 2, WrittenTokens: 30_000},
			},
			Tools: []ToolStatistics{
				{Name: "bash", Calls: 60, Failures: 4, Cancellations: 1, Took: 9 * time.Minute},
				{Name: "edit", Calls: 40, Failures: 1, Took: 20 * time.Second},
				{Name: "read", Calls: 20, Took: 2 * time.Second},
			},
		},
		{
			Name:      "wise-lemur",
			Provider:  "openrouter",
			Model:     "kimi-k2-thinking",
			StartedAt: startedAt.Add(5 * time.Hour),
			EndedAt:   startedAt.Add(5*time.Hour + 3*time.Minute),
			Cache: CacheStatistics{
				Sessions:        1,
				Requests:        1,
				Misses:          1,
				InputTokens:     12_000,
				OutputTokens:    150,
				PeakInputTokens: 12_000,
			},
			Activity: ActivityStatistics{
				Sessions:    1,
				Prompts:     1,
				SessionTime: 3 * time.Minute,
			},
			Faults: FaultStatistics{Sessions: 1, Failures: 1},
		},
		{
			Name:      "still-ibex",
			Provider:  "ollama",
			Model:     "qwen3.8:27b",
			StartedAt: startedAt.Add(6 * time.Hour),
			EndedAt:   startedAt.Add(6*time.Hour + 12*time.Minute),
			Cache: CacheStatistics{
				Sessions:        1,
				Requests:        4,
				Misses:          4,
				InputTokens:     60_000,
				OutputTokens:    2_500,
				PeakInputTokens: 20_000,
			},
			Activity: ActivityStatistics{
				Sessions:    1,
				Turns:       4,
				Prompts:     4,
				Replies:     4,
				ToolCalls:   6,
				TurnTime:    2 * time.Minute,
				LongestTurn: 45 * time.Second,
				SessionTime: 12 * time.Minute,
			},
			Faults: FaultStatistics{Sessions: 1},
			Tools:  []ToolStatistics{{Name: "read", Calls: 6, Took: 2 * time.Second}},
		},
	}
}

func analyseWithCache(directory string, cachePath string, names []string) (Analysis, error) {
	sessions, skippedCount, err := readSessions(directory, cachePath, names)
	if err != nil {
		return Analysis{}, err
	}

	analysis := aggregate(sessions, pricebook{})
	analysis.SkippedSessions = skippedCount

	return analysis, nil
}

func analyseOne(t *testing.T, directory string, name string) SessionStatistics {
	t.Helper()

	sessions, _, err := readSessions(directory, "", []string{name})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}

	return sessions[0]
}

func event(clockTime string, encodedEvent string) string {
	return fmt.Sprintf(`{"kind":"event","time":"2026-09-01T%sZ","event":%s}`, clockTime, encodedEvent)
}

func writeJournal(t *testing.T, directory string, name string, events ...string) {
	t.Helper()
	path := filepath.Join(directory, name, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-09-01T00:00:00Z","version":%d,"name":%q,"meta":{"provider":%q}}`,
		session.JournalFormat,
		name,
		journalProvider,
	)
	content := strings.Join(append([]string{head}, events...), "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()
	root, err := os.OpenRoot("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	if *updateGoldens {
		if err := root.WriteFile(name, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", name, drawn, want)
	}
}

func TestPricesAreReadFromTheModelCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	prices := agent.TokenPrices{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	err := model.StoreSimulated(path, "anthropic", []agent.Model{{
		ID:                  "claude-sonnet-4-6",
		Name:                "Sonnet 4.6",
		EffortLevels:        []string{"medium"},
		ContextWindowTokens: 200_000,
		Prices:              &prices,
	}})
	if err != nil {
		t.Fatal(err)
	}

	read := readPricebook(path)
	if got := read[priceKey("anthropic", "claude-sonnet-4-6")]; got != prices {
		t.Errorf("got prices %#v, want %#v", got, prices)
	}
	if len(readPricebook("")) != 0 {
		t.Error("an absent model cache should quote no prices")
	}
}
