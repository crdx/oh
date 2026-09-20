package sessions

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "oh"))
}

func TestGoldenTheListingMatchesTheGolden(t *testing.T) {
	var drawn strings.Builder
	if err := writeTable(goldenListings(time.Now()), &drawn); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "listing.txt", drawn.String())
}

func TestGoldenTheJSONListingMatchesTheGolden(t *testing.T) {
	var written strings.Builder
	fixed := time.Date(2026, time.August, 28, 14, 15, 25, 0, time.UTC)
	if err := writeJSON(goldenListings(fixed), &written); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "listing.json", written.String())
}

func TestGoldenAnEmptyListingMatchesTheGolden(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())

	var screen, failure strings.Builder
	output := console.Output{Screen: &screen, Failure: &failure}
	if err := run(&inputOpts{}, output); err != nil {
		t.Fatal(err)
	}
	if err := run(&inputOpts{JSON: true}, output); err != nil {
		t.Fatal(err)
	}

	if err := run(&inputOpts{Archived: true}, output); err != nil {
		t.Fatal(err)
	}
	if err := run(&inputOpts{Filter: "kimi"}, output); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "empty.txt", strings.Join([]string{
		"=== screen ===\n", screen.String(),
		"=== failure ===\n", failure.String(),
	}, ""))
}

func TestGoldenTheNoticeThatCapsTheListingMatchesTheGolden(t *testing.T) {
	var failure strings.Builder

	withinLimit(manyListings(listLimit), listLimit+7, "", &failure)
	withinLimit(manyListings(listLimit+7), listLimit+7, "ses", &failure)
	withinLimit(manyListings(listLimit+7), listLimit+7, "sessions", &failure)
	withinLimit(manyListings(listLimit), listLimit, "", &failure)

	assertGolden(t, "capped.txt", failure.String())
}

func goldenListings(now time.Time) []Listing {
	return []Listing{
		{
			Name:         "wild-scorpion",
			Status:       runningStatus,
			IsRunning:    true,
			Title:        "audit-golden-files 🟡",
			WorkspaceDir: "/home/agent/florp/io",
			ScratchDir:   "/state/farm/wild-scorpion",
			SessionDir:   "/state/sessions/wild-scorpion",
			Model:        "claude-opus-5",
			Effort:       "medium",
			Messages:     32,
			StartedAt:    now.Add(-7 * time.Hour),
			TouchedAt:    now.Add(-3 * time.Hour),
		},
		{
			Name:         "dewy-vole",
			Status:       endedStatus,
			IsFast:       true,
			Title:        strings.Repeat("a-title-far-wider-than-its-column ", 3),
			WorkspaceDir: "/home/agent/florp/io",
			ScratchDir:   "/state/farm/dewy-vole",
			SessionDir:   "/state/sessions/dewy-vole",
			Model:        "gpt-5.3-codex",
			Effort:       "high",
			Messages:     8,
			StartedAt:    now.Add(-90 * time.Minute),
			TouchedAt:    now.Add(-30 * time.Minute),
		},
		{
			Name:         "tame-impala",
			Status:       archivedStatus,
			IsArchived:   true,
			Title:        "rename the harness to oh",
			WorkspaceDir: "/home/agent/florp/io",
			ScratchDir:   "/state/farm/tame-impala",
			SessionDir:   "/state/sessions/tame-impala.tgz",
			Model:        "gpt-5.3-codex",
			Effort:       "medium",
			Messages:     26,
			StartedAt:    now.Add(-300 * time.Hour),
			TouchedAt:    now.Add(-290 * time.Hour),
		},
		{
			Name:         "chewy-raven",
			Status:       endedStatus,
			WorkspaceDir: "/home/agent/.system",
			ScratchDir:   "/state/farm/chewy-raven",
			SessionDir:   "/state/sessions/chewy-raven",
			StartedAt:    now,
			TouchedAt:    now,
		},
	}
}

func assertGolden(t *testing.T, name string, drawn string) {
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
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
