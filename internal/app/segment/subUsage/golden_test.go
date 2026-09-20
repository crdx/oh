package subUsage

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/usage"
	"crdx.org/io/pkg/agent"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

var (
	payloadPattern    = regexp.MustCompile(`;[A-Za-z0-9+/=]+\x1b\\`)
	identifierPattern = regexp.MustCompile(`i=\d+`)
	colourPattern     = regexp.MustCompile(`38;2;\d+;\d+;\d+m\x{10EEEE}`)
)

func window(duration time.Duration, percent float64, remainingTime time.Duration) agent.UsageWindow {
	return agent.UsageWindow{
		Duration: duration,
		Percent:  percent,
		ResetsAt: testNow.Add(remainingTime),
	}
}

func limited(duration time.Duration, percent float64, remainingTime time.Duration) agent.UsageWindow {
	built := window(duration, percent, remainingTime)
	built.IsLimited = true

	return built
}

func scoped(scope string, percent float64) agent.UsageWindow {
	built := window(5*time.Hour, percent, 2*time.Hour)
	built.Scope = scope

	return built
}

type segmentCase struct {
	name              string
	modelName         string
	windows           []agent.UsageWindow
	status            usageStatus
	statusBeforeFetch usageStatus
	failure           string
	fetchedAt         time.Time
	fetchStartedAt    time.Time
	timeStep          time.Duration
	isWaiting         bool
	hasImages         bool
	repaints          int
}

func segmentCases() []segmentCase {
	return []segmentCase{
		{
			name:    "windows on an even burn",
			windows: []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), window(7*24*time.Hour, 12, 6*24*time.Hour)},
		},
		{name: "a window ahead of its pace", windows: []agent.UsageWindow{window(5*time.Hour, 28, 4*time.Hour)}},
		{name: "a window far ahead of its pace", windows: []agent.UsageWindow{window(5*time.Hour, 60, 4*time.Hour)}},
		{name: "a window near its limit", windows: []agent.UsageWindow{window(5*time.Hour, 95, time.Hour)}},
		{
			name:      "a scoped window governing this model",
			modelName: "gpt-5.3-codex-spark",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), scoped("gpt-5.3-codex-spark", 70)},
		},
		{
			name:      "a scoped window governing another model",
			modelName: "claude-sonnet-4-6",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), scoped("opus", 70)},
		},
		{
			name:    "a window that is over its limit",
			windows: []agent.UsageWindow{{Duration: 30 * 24 * time.Hour, Percent: 100, IsLimited: true}},
		},
		{name: "a window whose reset has passed", windows: []agent.UsageWindow{window(5*time.Hour, 40, -time.Minute)}},
		{name: "a limited window whose reset has passed", windows: []agent.UsageWindow{limited(5*time.Hour, 100, -time.Minute)}},
		{
			name:    "a window with no reset at all",
			windows: []agent.UsageWindow{{Duration: 5 * time.Hour, Percent: 40}},
		},
		{
			name:              "a snapshot refreshing in the background",
			windows:           []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			status:            usageFetching,
			statusBeforeFetch: usageReady,
			fetchStartedAt:    testNow,
			timeStep:          spinnerDelay,
			repaints:          1,
		},
		{name: "nothing fetched yet", status: usagePending},
		{
			name:              "an initial fetch waiting for its spinner",
			status:            usageFetching,
			statusBeforeFetch: usagePending,
			fetchStartedAt:    testNow,
		},
		{
			name:              "an initial fetch showing its spinner",
			status:            usageFetching,
			statusBeforeFetch: usagePending,
			fetchStartedAt:    testNow.Add(-spinnerDelay),
		},
		{
			name:    "a fetch that was refused",
			windows: []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			status:  usageRetrying,
			failure: "429",
		},
		{name: "nothing to show at all"},
		{
			name:      "a snapshot past its refresh",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			fetchedAt: testNow.Add(-10 * time.Minute),
		},
		{
			name:      "a snapshot long past its refresh",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			fetchedAt: testNow.Add(-45 * time.Minute),
		},
		{
			name:      "a provider waiting on a turn of its own",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			fetchedAt: testNow.Add(-45 * time.Minute),
			isWaiting: true,
		},
		{
			name:      "windows drawn as pictures",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), window(7*24*time.Hour, 12, 6*24*time.Hour)},
			hasImages: true,
		},
		{
			name:      "windows drawn as pictures a second time",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), window(7*24*time.Hour, 12, 6*24*time.Hour)},
			hasImages: true,
			repaints:  1,
		},
		{
			name:      "two windows standing at the same figure",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), window(5*time.Hour, 40, 150*time.Minute)},
			hasImages: true,
		},
	}
}

func drawEachCase(t *testing.T, isPlain bool) string {
	t.Helper()

	var drawn strings.Builder

	for _, test := range segmentCases() {
		clock := &testClock{now: testNow}

		gauges := usage.NewGauges(nil)
		if test.hasImages {
			gauges = usage.FixedGauges(usage.Graphics{CellWidth: 9, CellHeight: 18})
		}

		segment := &state{
			modelName:         strings.ToLower(test.modelName),
			rate:              defaultRate,
			isSelfRefreshing:  !test.isWaiting,
			gauges:            gauges,
			now:               clock.read,
			windows:           test.windows,
			fetchStartedAt:    test.fetchStartedAt,
			status:            test.status,
			statusBeforeFetch: test.statusBeforeFetch,
		}

		fetchedAt := test.fetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = testNow
		}

		drawn.WriteString("=== ")
		drawn.WriteString(test.name)
		drawn.WriteString(" ===\n")

		for range test.repaints + 1 {
			text := segment.draw(snapshot{
				windows:   test.windows,
				fetchedAt: fetchedAt,
				status:    segment.getVisibleStatus(),
				failure:   test.failure,
			})

			if isPlain {
				text = style.Plain(text)
			}

			drawn.WriteString(withoutPayload(text))
			drawn.WriteString("\n")
			clock.set(clock.read().Add(test.timeStep))
		}
	}

	return drawn.String()
}

func withoutPayload(drawn string) string {
	drawn = payloadPattern.ReplaceAllString(drawn, ";<payload>\x1b\\")
	drawn = identifierPattern.ReplaceAllString(drawn, "i=<identifier>")

	return colourPattern.ReplaceAllString(drawn, "38;2;<identifier>m\U0010EEEE")
}

func TestGoldenEverySegmentMatchesTheGolden(t *testing.T) {
	checkGolden(t, "segment.txt", drawEachCase(t, true))
}

func TestGoldenEveryStyledSegmentMatchesTheGolden(t *testing.T) {
	checkGolden(t, "segment.ansi", drawEachCase(t, false))
}

func TestEveryGoldenIsClaimedByATest(t *testing.T) {
	claimed := map[string]struct{}{
		"segment.txt":  {},
		"segment.ansi": {},
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
