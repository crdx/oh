package usage

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/pkg/agent"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

var (
	payloadPattern    = regexp.MustCompile(`;[A-Za-z0-9+/=]+\x1b\\`)
	identifierPattern = regexp.MustCompile(`i=\d+`)
	colourPattern     = regexp.MustCompile(`38;2;\d+;\d+;\d+m\x{10EEEE}`)
)

type drawnCase struct {
	name    string
	sources []Source
	at      time.Time
}

func limitedWindow(percent float64) agent.UsageWindow {
	return agent.UsageWindow{Duration: 30 * 24 * time.Hour, Percent: percent, IsLimited: true}
}

func limitedWeeklyWindow() agent.UsageWindow {
	window := weeklyWindow(100)
	window.IsLimited = true

	return window
}

func scopedWindow(scope string, percent float64) agent.UsageWindow {
	return agent.UsageWindow{
		Duration: 5 * time.Hour,
		Percent:  percent,
		ResetsAt: collectedAt.Add(90 * time.Minute),
		Scope:    scope,
	}
}

func passedWindow() agent.UsageWindow {
	return agent.UsageWindow{
		Duration: 5 * time.Hour,
		Percent:  99,
		ResetsAt: collectedAt.Add(time.Second),
	}
}

func passedLimitedWindow() agent.UsageWindow {
	window := passedWindow()
	window.Percent = 100
	window.IsLimited = true

	return window
}

func reporting(windows ...agent.UsageWindow) *scriptedReporter {
	return &scriptedReporter{windows: windows}
}

func everyProvider() []Source {
	return []Source{
		{
			Provider:             "anthropic",
			Label:                "Anthropic",
			Reporter:             reporting(weeklyWindow(51), scopedWindow("opus", 88)),
			HasIdleSessionWindow: true,
			IsSelfRefreshing:     true,
		},
		{
			Provider: "codex",
			Label:    "OpenAI",
			Reporter: reporting(sessionWindow(41), weeklyWindow(64), scopedWindow("gpt-5.3-codex-spark", 12)),
		},
		{
			Provider:         "opencode-go",
			Label:            "OpenCode Go",
			Reporter:         reporting(sessionWindow(3), limitedWindow(100)),
			IsSelfRefreshing: true,
		},
	}
}

func drawnCases() []drawnCase {
	minuteLater := collectedAt.Add(time.Minute)

	return []drawnCase{
		{name: "every provider", sources: everyProvider(), at: minuteLater},
		{
			name: "a provider that was refused",
			sources: []Source{{
				Provider: "anthropic",
				Label:    "Anthropic",
				Reporter: &scriptedReporter{err: refusal(429)},
			}},
			at: minuteLater,
		},
		{
			name: "a scope nothing has been spent on",
			sources: []Source{{
				Provider: "codex",
				Label:    "OpenAI",
				Reporter: reporting(sessionWindow(41), scopedWindow("gpt-5.3-codex-spark", 0)),
			}},
			at: minuteLater,
		},
		{
			name: "a window whose reset is unknown",
			sources: []Source{{
				Provider: "codex",
				Label:    "OpenAI",
				Reporter: reporting(agent.UsageWindow{Duration: 5 * time.Hour, Percent: 37}),
			}},
			at: minuteLater,
		},
		{
			name:    "a provider with nothing to report",
			sources: []Source{{Provider: "codex", Label: "OpenAI", Reporter: reporting()}},
			at:      minuteLater,
		},
		{
			name:    "a window that has run out",
			sources: []Source{{Provider: "codex", Label: "OpenAI", Reporter: reporting(passedWindow())}},
			at:      collectedAt.Add(2 * time.Minute),
		},
		{
			name: "a limited window that still has a reset",
			sources: []Source{{
				Provider: "codex",
				Label:    "OpenAI",
				Reporter: reporting(sessionWindow(41), limitedWeeklyWindow()),
			}},
			at: minuteLater,
		},
		{
			name:    "a limited window whose reset has passed",
			sources: []Source{{Provider: "codex", Label: "OpenAI", Reporter: reporting(passedLimitedWindow())}},
			at:      collectedAt.Add(2 * time.Minute),
		},
		{
			name:    "a snapshot past its refresh",
			sources: []Source{{Provider: "codex", Label: "OpenAI", Reporter: reporting(sessionWindow(20))}},
			at:      collectedAt.Add(10 * time.Minute),
		},
		{
			name: "a snapshot long past its refresh",
			sources: []Source{{
				Provider:         "anthropic",
				Label:            "Anthropic",
				Reporter:         reporting(sessionWindow(20)),
				IsSelfRefreshing: true,
			}},
			at: collectedAt.Add(45 * time.Minute),
		},
		{
			name:    "a provider waiting on a turn of its own",
			sources: []Source{{Provider: "codex", Label: "OpenAI", Reporter: reporting(sessionWindow(20))}},
			at:      collectedAt.Add(45 * time.Minute),
		},
		{name: "nothing to report at all", at: minuteLater},
	}
}

func drawEachCase(t *testing.T, isPlain bool) string {
	t.Helper()

	var drawn strings.Builder

	for _, test := range drawnCases() {
		report := Collect(t.Context(), test.sources, nowAt(collectedAt))

		text := Render(report, test.at, nil)
		if isPlain {
			text = style.Plain(text)
		}

		drawn.WriteString("=== ")
		drawn.WriteString(test.name)
		drawn.WriteString(" ===\n")
		drawn.WriteString(text)
	}

	return drawn.String()
}

func TestGoldenEveryViewMatchesTheGolden(t *testing.T) {
	checkGolden(t, "views.txt", drawEachCase(t, true))
}

func TestGoldenEveryStyledViewMatchesTheGolden(t *testing.T) {
	checkGolden(t, "views.ansi", drawEachCase(t, false))
}

func seededCache(t *testing.T, nextProbeAt time.Time) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "usage", "codex.json")
	fetchedAt := collectedAt.Add(-23 * time.Hour)

	err := newCacheStore(path).update(func(storedCache *cache) error {
		storedCache.Version = cacheFormat
		storedCache.FetchedAt = fetchedAt
		storedCache.Windows = []agent.UsageWindow{
			{
				Duration: 5 * time.Hour,
				Percent:  96,
				ResetsAt: collectedAt.Add(-45 * time.Minute),
			},
			limitedWeeklyWindow(),
		}
		storedCache.Probe = &probeState{AttemptedAt: fetchedAt, NextAt: nextProbeAt}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return path
}

func refreshedReporter() *scriptedProbeReporter {
	return &scriptedProbeReporter{
		probe: agent.UsageProbe{
			Windows:      []agent.UsageWindow{sessionWindow(6), weeklyWindow(88)},
			Availability: agent.UsageAvailabilityAllowed,
		},
	}
}

func laterResetReporter() *scriptedProbeReporter {
	laterWeek := weeklyWindow(88)
	laterWeek.ResetsAt = laterWeek.ResetsAt.Add(time.Hour)

	return &scriptedProbeReporter{
		probe: agent.UsageProbe{
			Windows:      []agent.UsageWindow{sessionWindow(6), laterWeek},
			Availability: agent.UsageAvailabilityAllowed,
		},
	}
}

func silentReporter() *scriptedProbeReporter {
	return &scriptedProbeReporter{
		probe: agent.UsageProbe{Availability: agent.UsageAvailabilityAllowed},
	}
}

func resetlessReporter() *scriptedProbeReporter {
	weekWithoutReset := weeklyWindow(88)
	weekWithoutReset.ResetsAt = time.Time{}

	return &scriptedProbeReporter{
		probe: agent.UsageProbe{
			Windows:      []agent.UsageWindow{sessionWindow(6), weekWithoutReset},
			Availability: agent.UsageAvailabilityAllowed,
		},
	}
}

func drawEachStandingLimit(t *testing.T, isPlain bool) string {
	t.Helper()

	var drawn strings.Builder

	for _, test := range []struct {
		name        string
		nextProbeAt time.Time
		reporter    *scriptedProbeReporter
	}{
		{
			name:        "a standing limit whose probe is not due yet",
			nextProbeAt: collectedAt.Add(10 * time.Minute),
			reporter:    refreshedReporter(),
		},
		{
			name:        "a standing limit whose probe has fallen due",
			nextProbeAt: collectedAt.Add(-10 * time.Minute),
			reporter:    refreshedReporter(),
		},
		{
			name:        "a refreshed window uses its later reset",
			nextProbeAt: collectedAt.Add(-10 * time.Minute),
			reporter:    laterResetReporter(),
		},
		{
			name:        "a refreshed window that arrived with no reset",
			nextProbeAt: collectedAt.Add(-10 * time.Minute),
			reporter:    resetlessReporter(),
		},
		{
			name:        "a limit lifted by a probe that named no window",
			nextProbeAt: collectedAt.Add(-10 * time.Minute),
			reporter:    silentReporter(),
		},
	} {
		report := Collect(t.Context(), []Source{{
			Provider:         "codex",
			Label:            "OpenAI",
			Reporter:         test.reporter,
			CachePath:        seededCache(t, test.nextProbeAt),
			IsSelfRefreshing: true,
		}}, nowAt(collectedAt))

		text := Render(report, collectedAt, nil)
		if isPlain {
			text = style.Plain(text)
		}

		drawn.WriteString("=== ")
		drawn.WriteString(test.name)
		drawn.WriteString(" ===\n")
		drawn.WriteString(text)
	}

	return drawn.String()
}

func TestGoldenASnapshotUnderAStandingLimitMatchesTheGolden(t *testing.T) {
	checkGolden(t, "standing-limit.txt", drawEachStandingLimit(t, true))
}

func TestGoldenAStyledSnapshotUnderAStandingLimitMatchesTheGolden(t *testing.T) {
	checkGolden(t, "standing-limit.ansi", drawEachStandingLimit(t, false))
}

func TestGoldenProviderLimitFailuresMatchTheGolden(t *testing.T) {
	windows := []agent.UsageWindow{
		{Duration: 5 * time.Hour, ResetsAt: collectedAt.Add(time.Hour), IsLimited: true},
		{Duration: 7 * 24 * time.Hour, ResetsAt: collectedAt.Add(2 * time.Hour), IsLimited: true},
	}

	text := limitedError("Codex", windows, collectedAt).Error() + "\n"
	text += limitedError("Anthropic", []agent.UsageWindow{{IsLimited: true}}, collectedAt).Error() + "\n"
	checkGolden(t, "limits.txt", text)
}

func TestGoldenEveryDrawnGaugeMatchesTheGolden(t *testing.T) {
	drawing := Graphics{CellWidth: 9, CellHeight: 18}
	expected := 40

	var drawn strings.Builder

	for _, test := range []struct {
		name     string
		limit    Limit
		pace     Pace
		cells    int
		repaints int
	}{
		{name: "an idle limit", limit: Limit{}, cells: gaugeWidth},
		{name: "a limit within its pace", limit: Limit{UsedPercent: 25, ExpectedPercent: &expected}, cells: gaugeWidth},
		{
			name:  "a limit ahead of its pace",
			limit: Limit{UsedPercent: 62, ExpectedPercent: &expected},
			pace:  PaceAhead,
			cells: gaugeWidth,
		},
		{
			name:  "a limit far ahead of its pace",
			limit: Limit{UsedPercent: 95, ExpectedPercent: &expected},
			pace:  PaceCritical,
			cells: gaugeWidth,
		},
		{name: "a full limit", limit: Limit{UsedPercent: 100}, pace: PaceCritical, cells: gaugeWidth},
		{
			name:  "a narrow gauge",
			limit: Limit{UsedPercent: 62, ExpectedPercent: &expected},
			pace:  PaceAhead,
			cells: gaugeWidth / 2,
		},
		{
			name:     "a gauge painted a second time",
			limit:    Limit{UsedPercent: 62, ExpectedPercent: &expected},
			pace:     PaceAhead,
			cells:    gaugeWidth,
			repaints: 1,
		},
	} {
		gauges := FixedGauges(drawing)

		drawn.WriteString("=== ")
		drawn.WriteString(test.name)
		drawn.WriteString(" ===\n")

		for range test.repaints + 1 {
			placement, isPlaced := gauges.place(
				test.limit.UsedPercent, test.limit.ExpectedPercent, test.pace, test.cells, drawing,
			)
			if !isPlaced {
				t.Fatalf("%s was not placed", test.name)
			}

			drawn.WriteString(withoutPayload(placement))
			drawn.WriteString("\n")
		}

		drawn.WriteString(describePicture(gaugeImage(
			test.limit.UsedPercent, test.limit.ExpectedPercent, test.pace, test.cells, drawing,
		)))
	}

	checkGolden(t, "gauges.txt", drawn.String())
}

func TestGoldenTheCollectionMatchesTheGolden(t *testing.T) {
	sources := append(everyProvider(),
		Source{Provider: "anthropic", Label: "Anthropic", Reporter: &scriptedReporter{err: refusal(500)}},
		Source{Provider: "ollama", Label: "Ollama", Reporter: reporting()},
	)

	report := Collect(t.Context(), sources, nowAt(collectedAt))

	document, err := json.MarshalIndent(report, "", "    ")
	if err != nil {
		t.Fatal(err)
	}

	checkGolden(t, "report.json", string(document)+"\n")
}

func TestEveryGoldenIsClaimedByATest(t *testing.T) {
	claimed := map[string]struct{}{
		"views.txt":           {},
		"views.ansi":          {},
		"standing-limit.txt":  {},
		"standing-limit.ansi": {},
		"limits.txt":          {},
		"gauges.txt":          {},
		"report.json":         {},
	}

	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if _, isClaimed := claimed[entry.Name()]; !isClaimed {
			t.Errorf("nothing draws testdata/%s", entry.Name())
		}

		delete(claimed, entry.Name())
	}

	for name := range claimed {
		t.Errorf("testdata/%s was never written", name)
	}
}

func describePicture(picture *image.RGBA) string {
	var described strings.Builder

	bounds := picture.Bounds()
	previous := ""
	firstRow := 0

	for y := bounds.Min.Y; y <= bounds.Max.Y; y++ {
		current := ""
		if y < bounds.Max.Y {
			current = describeRow(picture, y)
		}

		if y > bounds.Min.Y && current != previous {
			fmt.Fprintf(&described, "rows %d-%d: %s\n", firstRow, y-1, previous)
			firstRow = y
		}

		previous = current
	}

	return described.String()
}

func describeRow(picture *image.RGBA, y int) string {
	var runs []string

	bounds := picture.Bounds()
	runLength := 0
	previous := picture.RGBAAt(bounds.Min.X, y)

	for x := bounds.Min.X; x <= bounds.Max.X; x++ {
		if x < bounds.Max.X && picture.RGBAAt(x, y) == previous {
			runLength++

			continue
		}

		runs = append(runs, fmt.Sprintf("%dx%02x%02x%02x%02x", runLength, previous.R, previous.G, previous.B, previous.A))

		if x < bounds.Max.X {
			previous = picture.RGBAAt(x, y)
			runLength = 1
		}
	}

	return strings.Join(runs, " ")
}

func withoutPayload(placement string) string {
	placement = payloadPattern.ReplaceAllString(placement, ";<payload>\x1b\\")
	placement = identifierPattern.ReplaceAllString(placement, "i=<identifier>")

	return colourPattern.ReplaceAllString(placement, "38;2;<identifier>m\U0010EEEE")
}

func checkGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if drawn != string(want) {
		t.Errorf("what was drawn differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
