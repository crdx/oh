package bar

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util/strutil"
)

type fixedSegment string

func (self fixedSegment) Render(segment.Context) string {
	return string(self)
}

func fixedFactory(value string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return fixedSegment(value), nil
	}
}

type fittingSegment struct {
	availableCells *int
}

func (self fittingSegment) Render(segment.Context) string {
	return "unbounded"
}

func (self fittingSegment) RenderWithin(_ segment.Context, cells int) string {
	*self.availableCells = cells
	return "+50"
}

func TestTheFastModeSegmentIsRegisteredWithTheCurrentSelection(t *testing.T) {
	for _, isFast := range []bool{false, true} {
		registry := NewRegistry(Options{Workspace: work.At(t.TempDir()), IsFast: isFast})
		builtSegment, err := registry[fastModeSegment](nil)
		if err != nil {
			t.Fatal(err)
		}

		got := style.Plain(builtSegment.Render(segment.Context{}))
		if isFast && got != "⚡" || !isFast && got != "·" {
			t.Errorf("fast=%t drew %q", isFast, got)
		}
	}
}

func TestInfoDrawsEveryAvailableNonemptySegmentAndSummarisesTheEmptyOnes(t *testing.T) {
	cacheValue := "\x1b[31m5m ttl\x1b[0m"
	modeValue := "\x1b[32mrxw ngl\x1b[0m"
	registry := segment.Registry{
		activitySpinnerSegment: fixedFactory("·✦·"),
		cacheUsageSegment:      fixedFactory("unused configured value"),
		jobNamesSegment:        fixedFactory(""),
		modeToggleSegment:      fixedFactory(modeValue),
		scrollOverflowSegment:  fixedFactory("↑ 3"),
	}
	configuration := NewConfiguration(registry, segment.Layout{
		segment.TopCenter: {
			segment.Instance{Name: cacheUsageSegment, Segment: fixedSegment(cacheValue)},
		},
	})

	info, err := configuration.RenderInfo(segment.Context{})
	if err != nil {
		t.Fatal(err)
	}
	got := strutil.VisibleEscapes(info) + "\n"
	want, err := os.ReadFile(filepath.Join("testdata", "info.ansi"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderWithinHandsAFittingSegmentOnlyTheRoomThatRemains(t *testing.T) {
	availableCells := 0
	layout := segment.Layout{
		segment.TopLeft: {
			segment.Instance{Name: "fixed", Segment: fixedSegment("abc")},
			segment.Instance{
				Name:    "fitting",
				Segment: fittingSegment{availableCells: &availableCells},
			},
		},
	}

	got := RenderWithin(layout, segment.TopLeft, segment.Context{}, 10)
	if availableCells != 4 {
		t.Errorf("fitting segment got %d cells, want 4", availableCells)
	}
	if style.Plain(got) != "abc ─ +50" || style.Width(got) > 10 {
		t.Errorf("got %q at width %d", style.Plain(got), style.Width(got))
	}
}

func TestTheSegmentSeparatorFollowsTheActiveTheme(t *testing.T) {
	theme := style.DefaultTheme()
	theme.Dim = "#010203"
	restoreTheme := style.ApplyTheme(theme)
	defer restoreTheme()

	layout := segment.Layout{
		segment.TopLeft: {
			segment.Instance{Name: "first", Segment: fixedSegment("abc")},
			segment.Instance{Name: "second", Segment: fixedSegment("def")},
		},
	}

	got := Render(layout, segment.TopLeft, segment.Context{})
	if want := " " + style.Dim("─") + " "; !strings.Contains(got, want) {
		t.Errorf("got %q, want the separator drawn as %q", got, want)
	}
}

func TestEverySegmentTheDefaultsNameIsRegistered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, fmt.Appendf(nil, "version = %d\n", config.Format), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry(Options{Workspace: work.At(t.TempDir())})
	if _, err := settings.BuildLayout(registry); err != nil {
		t.Fatalf("the built-in layout names a segment nothing supplies: %v", err)
	}
}
