package usage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

var collectedAt = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

type scriptedReporter struct {
	windows []agent.UsageWindow
	err     error
	asked   atomic.Int64
}

func (self *scriptedReporter) IsAvailable() bool {
	return true
}

func (self *scriptedReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.asked.Add(1)

	return self.windows, self.err
}

type scriptedProbeReporter struct {
	scriptedReporter

	probe  agent.UsageProbe
	probed atomic.Int64
}

func (self *scriptedProbeReporter) ProbeUsage(context.Context) (agent.UsageProbe, error) {
	self.probed.Add(1)

	return self.probe, nil
}

func refusal(status int) error {
	return &req.StatusError{Status: status}
}

func nowAt(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func sessionWindow(percent float64) agent.UsageWindow {
	return agent.UsageWindow{
		Duration: 5 * time.Hour,
		Percent:  percent,
		ResetsAt: collectedAt.Add(2 * time.Hour),
	}
}

func weeklyWindow(percent float64) agent.UsageWindow {
	return agent.UsageWindow{
		Duration: 7 * 24 * time.Hour,
		Percent:  percent,
		ResetsAt: collectedAt.Add(48 * time.Hour),
	}
}

func TestAProviderWithNothingToSayIsLeftOut(t *testing.T) {
	report := Collect(t.Context(), []Source{
		{Provider: "codex", Label: "OpenAI"},
		{Provider: "anthropic", Label: "Anthropic", Reporter: &scriptedReporter{}},
	}, nowAt(collectedAt))

	if len(report.Providers) != 1 {
		t.Fatalf("collected %d providers, want 1", len(report.Providers))
	}

	if report.Providers[0].Provider.Label != "Anthropic" {
		t.Errorf("collected %q", report.Providers[0].Provider.Label)
	}

	if report.Providers[0].Status != StatusUnavailable {
		t.Errorf("status is %q, want %q", report.Providers[0].Status, StatusUnavailable)
	}
}

func TestARefusedFetchIsReportedWithItsStatus(t *testing.T) {
	report := Collect(t.Context(), []Source{{
		Provider: "anthropic",
		Label:    "Anthropic",
		Reporter: &scriptedReporter{err: refusal(429)},
	}}, nowAt(collectedAt))

	if got := report.Providers[0].Status; got != StatusFailed {
		t.Fatalf("status is %q, want %q", got, StatusFailed)
	}

	if got := report.Providers[0].Message; got != "refused with 429" {
		t.Errorf("message is %q", got)
	}
}

func TestASessionWindowIsShownAsIdleWhereAProviderOmitsIt(t *testing.T) {
	report := Collect(t.Context(), []Source{{
		Provider:             "anthropic",
		Label:                "Anthropic",
		Reporter:             &scriptedReporter{windows: []agent.UsageWindow{weeklyWindow(20)}},
		HasIdleSessionWindow: true,
	}}, nowAt(collectedAt))

	limits := report.Providers[0].Limits
	if len(limits) != 2 {
		t.Fatalf("collected %d limits, want 2", len(limits))
	}

	if limits[0].Label != "Session" || limits[0].IsActive || limits[0].UsedPercent != 0 {
		t.Errorf("the idle session limit reads %+v", limits[0])
	}
}

func TestTheWindowsGoverningEverythingComeFirst(t *testing.T) {
	scoped := agent.UsageWindow{
		Duration: 5 * time.Hour,
		Percent:  4,
		ResetsAt: collectedAt.Add(time.Hour),
		Scope:    "gpt-5.3-codex-spark",
	}

	report := Collect(t.Context(), []Source{{
		Provider: "codex",
		Label:    "OpenAI",
		Reporter: &scriptedReporter{windows: []agent.UsageWindow{scoped, weeklyWindow(20), sessionWindow(10)}},
	}}, nowAt(collectedAt))

	var labels []string
	for _, limit := range report.Providers[0].Limits {
		labels = append(labels, limit.Label)
	}

	want := []string{"Session", "Week", "Spark Session"}
	for i, label := range want {
		if labels[i] != label {
			t.Fatalf("limits read %v, want %v", labels, want)
		}
	}
}

func TestACachedSnapshotIsNotFetchedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage", "codex.json")
	reporter := &scriptedReporter{windows: []agent.UsageWindow{sessionWindow(30)}}

	source := Source{Provider: "codex", Label: "OpenAI", Reporter: reporter, CachePath: path}

	Collect(t.Context(), []Source{source}, nowAt(collectedAt))
	report := Collect(t.Context(), []Source{source}, nowAt(collectedAt.Add(time.Minute)))

	if asked := reporter.asked.Load(); asked != 1 {
		t.Errorf("the reporter was asked %d times, want 1", asked)
	}

	if got := report.Providers[0].Limits[0].UsedPercent; got != 30 {
		t.Errorf("the cached limit reads %d%%, want 30%%", got)
	}

	if got := report.Providers[0].MeasuredAt; got != collectedAt.Format(time.RFC3339) {
		t.Errorf("measured at %q, want the moment it was fetched", got)
	}
}

func TestTheCollectionIsWrittenAsOneVersionedDocument(t *testing.T) {
	report := Collect(t.Context(), []Source{{
		Provider: "codex",
		Label:    "OpenAI",
		Reporter: &scriptedReporter{windows: []agent.UsageWindow{sessionWindow(40)}},
	}}, nowAt(collectedAt))

	document, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"schema_version":1,"providers":[{"provider":{"id":"codex","label":"OpenAI"},` +
		`"status":"ok","measured_at":"2026-01-02T12:00:00Z","age_seconds":0,` +
		`"refresh_after_seconds":300,"fresh_within_seconds":360,"stale_after_seconds":1800,` +
		`"is_self_refreshing":false,"freshness":"fresh",` +
		`"limits":[{"id":"5h","label":"Session","short_label":"5h","window_seconds":18000,` +
		`"used_percent":40,"expected_percent":60,"pace_delta":-20,` +
		`"resets_at":"2026-01-02T14:00:00Z","remaining_seconds":7200,"is_active":true,` +
		`"state":"active","severity":"normal"}]}]}`

	if string(document) != want {
		t.Errorf("collected\n%s\nwant\n%s", document, want)
	}
}
