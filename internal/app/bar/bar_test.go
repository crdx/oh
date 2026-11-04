package bar

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/approvals"
	"crdx.org/oh/internal/app/segment/localTime"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/question"
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

type fittingSegment []string

func (self fittingSegment) Render(segment.Context) string {
	if len(self) == 0 {
		return ""
	}

	return self[0]
}

func (self fittingSegment) Ladder(segment.Context) []string {
	return self
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
	modeValue := "\x1b[32mrxw snlg\x1b[0m"
	registry := segment.Registry{
		activitySpinnerSegment: fixedFactory("·✦·"),
		cacheUsageSegment:      fixedFactory("unused configured value"),
		taskNamesSegment:       fixedFactory(""),
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

func TestDetailIsShedFromTheRightBeforeAnySegmentIsDroppedFromTheLeft(t *testing.T) {
	layout := segment.Layout{
		segment.TopLeft: {
			segment.Instance{Name: "first", Segment: fittingSegment{"alpha", "al", ""}},
			segment.Instance{Name: "second", Segment: fittingSegment{"bravo", "br", ""}},
		},
	}

	for _, test := range []struct {
		cells int
		want  string
	}{
		{cells: 13, want: "alpha ─ bravo"},
		{cells: 12, want: "alpha ─ br"},
		{cells: 9, want: "al ─ br"},
		{cells: 4, want: "br"},
		{cells: 1, want: ""},
	} {
		got := RenderWithin(layout, segment.TopLeft, segment.Context{}, test.cells)
		if style.Plain(got) != test.want || style.Width(got) > test.cells {
			t.Errorf(
				"%d cells drew %q at width %d, want %q",
				test.cells,
				style.Plain(got),
				style.Width(got),
				test.want,
			)
		}
	}
}

func TestAnUnboundedPositionDrawsTheRichestRungOfEverySegment(t *testing.T) {
	layout := segment.Layout{
		segment.TopLeft: {
			segment.Instance{Name: "first", Segment: fittingSegment{"alpha", "al", ""}},
			segment.Instance{Name: "second", Segment: fixedSegment("bravo")},
		},
	}

	if got := style.Plain(Render(layout, segment.TopLeft, segment.Context{})); got != "alpha ─ bravo" {
		t.Errorf("got %q", got)
	}
}

func TestTheSegmentSeparatorFollowsTheActiveTheme(t *testing.T) {
	theme := style.DefaultTheme()
	theme.Dark.Dim = "#010203"
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

func TestTheForwardsAndGrantsSegmentsReplaceTheRetiredOnes(t *testing.T) {
	registry := NewRegistry(Options{Workspace: work.At(t.TempDir())})

	if _, isRegistered := registry[forwardsSegment]; !isRegistered {
		t.Errorf("no segment is registered as %q", forwardsSegment)
	}
	if _, isRegistered := registry[grantsSegment]; !isRegistered {
		t.Errorf("no segment is registered as %q", grantsSegment)
	}
	if _, isRegistered := registry["exposed-ports"]; isRegistered {
		t.Error("the retired exposed-ports segment is still registered")
	}
	if _, isRegistered := registry["forwarded-ports"]; isRegistered {
		t.Error("the retired forwarded-ports segment is still registered")
	}
	if _, isRegistered := registry["path-grants"]; isRegistered {
		t.Error("the retired path-grants segment is still registered")
	}
}

func TestInfoLeavesTheStylingOfEveryRealSegmentToTheSegment(t *testing.T) {
	granted, err := caps.Parse("rx")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(Options{
		Workspace:         work.At("/workspace"),
		Session:           cycle.Session{Name: "tame-impala", Model: "claude-haiku-5-5", Effort: "high"},
		ModelEffortLevels: []string{"low", "medium", "high", "max"},
		Sources: Sources{
			IsTurnRunning:      func() bool { return false },
			IsSessionPersisted: func() bool { return true },
			GetContextUsage:    func() (int, int) { return 48_000, 1_000_000 },
			GetCacheUsage:      func() (int, int) { return 91, 100 },
			GetSessionSpend:    func() (float64, bool) { return 0.01, true },
			GetGrantedCaps:     func() caps.Set { return granted },
			GetGroupStatus:     func() caps.GroupStatus { return caps.GroupStatus{} },
			GetPathGrants:      func() []pathgrant.Grant { return nil },
			GetForwardedRoutes: func() []portgrant.Route { return nil },
			IsPrefixPending:    func() bool { return false },
			GetTurnTiming:      func() turn.Timing { return turn.Timing{UserTurn: time.Minute, ModelTurn: 2 * time.Minute} },
			GetTurnCount:       func() int { return 6 },
		},
	})
	drawnAt := time.Date(2026, time.October, 8, 10, 8, 0, 0, time.Local)
	registry[localTimeSegment] = localTime.New(func() time.Time { return drawnAt })
	registry[approvalsSegment] = approvals.New(func() []question.RememberedAnswer {
		return []question.RememberedAnswer{
			{Key: "network:host", Option: question.Option{Label: "Allow for session", RememberForSession: true}},
			{Key: "tool:fetch", Option: question.Option{Label: "Allow for 5 mins"}, ExpiresAt: drawnAt.Add(4 * time.Minute)},
		}
	}, func() time.Time { return drawnAt })
	configuration := NewConfiguration(registry, segment.Layout{})

	info, err := configuration.RenderInfo(segment.Context{})
	if err != nil {
		t.Fatal(err)
	}
	got := strutil.VisibleEscapes(info) + "\n"
	want, err := os.ReadFile(filepath.Join("testdata", "info-segments.ansi"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestImportantSegmentsKeepTheirDetailUntilOrdinarySegmentsDisappear(t *testing.T) {
	for _, importantAt := range []int{0, 1} {
		instances := []segment.Segment{
			fittingSegment{"ordinary", "ord", ""},
			fittingSegment{"ordinary", "ord", ""},
		}
		instances[importantAt] = segment.Instance{
			Segment:  fittingSegment{"important", "imp", ""},
			Priority: segment.Important,
		}
		layout := segment.Layout{segment.TopLeft: instances}
		for _, test := range []struct {
			cells int
			want  string
		}{
			{cells: 14, want: "important"},
			{cells: 9, want: "important"},
			{cells: 8, want: "imp"},
			{cells: 2, want: ""},
		} {
			if got := style.Plain(RenderWithin(layout, segment.TopLeft, segment.Context{}, test.cells)); got != test.want {
				t.Errorf("important at %d in %d cells: got %q, want %q", importantAt, test.cells, got, test.want)
			}
		}
	}
}

func TestImportantSegmentsTakeSpaceAcrossTheWholeRuler(t *testing.T) {
	positions := [3]segment.Position{segment.TopLeft, segment.TopCenter, segment.TopRight}
	for _, importantPosition := range positions {
		layout := segment.Layout{}
		for _, position := range positions {
			layout[position] = []segment.Segment{fittingSegment{"ordinary detail", "ord", ""}}
		}
		layout[importantPosition] = []segment.Segment{segment.Instance{
			Segment:  fittingSegment{"important", "imp", ""},
			Priority: segment.Important,
		}}
		for cells := range 101 {
			ruler := RenderRuler(layout, positions, segment.Context{}, cells)
			if !ruler.Fits(cells) {
				t.Fatalf("%s in %d cells does not fit: %+v", importantPosition, cells, ruler)
			}
			if cells >= 13 && !strings.Contains(ruler.Left+ruler.Center+ruler.Right, "important") {
				t.Fatalf("%s lost its detail in %d cells: %+v", importantPosition, cells, ruler)
			}
		}
	}
}

func TestRulerOverridesDoNotReplaceTheStoredLayout(t *testing.T) {
	positions := [3]segment.Position{segment.TopLeft, segment.TopCenter, segment.TopRight}
	configuration := NewConfiguration(nil, segment.Layout{
		segment.TopCenter: {segment.Text("ordinary")},
	})
	overrides := segment.Layout{
		segment.TopCenter: {segment.Instance{Segment: segment.Text("important"), Priority: segment.Important}},
	}
	if got := configuration.RenderRuler(positions, segment.Context{}, 40, overrides).Center; got != "important" {
		t.Errorf("override drew %q", got)
	}
	if got := configuration.RenderRuler(positions, segment.Context{}, 40, nil).Center; got != "ordinary" {
		t.Errorf("stored centre drew %q", got)
	}
}

func TestEqualPriorityPlacementsKeepTheRightBeforeTheLeftAndCentre(t *testing.T) {
	positions := [3]segment.Position{segment.BottomLeft, segment.BottomCenter, segment.BottomRight}
	for _, priority := range []segment.Priority{segment.Normal, segment.Important} {
		layout := segment.Layout{
			segment.BottomLeft:   {segment.Instance{Segment: segment.Text("left"), Priority: priority}},
			segment.BottomCenter: {segment.Instance{Segment: segment.Text("centre"), Priority: priority}},
			segment.BottomRight:  {segment.Instance{Segment: segment.Text("right"), Priority: priority}},
		}
		for _, test := range []struct {
			cells  int
			left   string
			center string
			right  string
		}{
			{cells: 27, left: "left", center: "centre", right: "right"},
			{cells: 18, left: "left", right: "right"},
			{cells: 17, right: "right"},
			{cells: 8, left: "left"},
			{cells: 7},
		} {
			ruler := RenderRuler(layout, positions, segment.Context{}, test.cells)
			if ruler.Left != test.left || ruler.Center != test.center || ruler.Right != test.right {
				t.Errorf("priority %d in %d cells: %+v", priority, test.cells, ruler)
			}
		}
	}
}

func TestSegmentsUseTheSpaceThatAnOversizedPlacementCannotUse(t *testing.T) {
	positions := [3]segment.Position{segment.TopLeft, segment.TopCenter, segment.TopRight}
	layout := segment.Layout{
		segment.TopLeft:   {segment.Text("too wide to fit")},
		segment.TopCenter: {segment.Text("centre")},
		segment.TopRight:  {segment.Text("too wide to fit")},
	}
	ruler := RenderRuler(layout, positions, segment.Context{}, 8)
	if ruler.Left != "" || ruler.Center != "centre" || ruler.Right != "" {
		t.Errorf("unused space did not reach the centre: %+v", ruler)
	}
}
