package usage

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/pkg/agent"
)

func TestGaugeColoursFollowTheTheme(t *testing.T) {
	theme := style.DefaultTheme()
	theme.Dim = "#0a0c0e"
	theme.StatusWarning = "#010203"
	theme.StatusInfo = "#040506"
	theme.StatusDanger = "#070809"
	restoreTheme := style.ApplyTheme(theme)
	defer restoreTheme()

	for pace, want := range map[Pace]color.RGBA{
		PaceAhead:    {R: 1, G: 2, B: 3, A: 0xff},
		PaceEven:     {R: 4, G: 5, B: 6, A: 0xff},
		PaceCritical: {R: 7, G: 8, B: 9, A: 0xff},
	} {
		if got := paceColour(pace); got != want {
			t.Errorf("pace %d colour = %+v, want %+v", pace, got, want)
		}
	}

	picture := gaugeImage(0, nil, PaceEven, 1, Graphics{CellWidth: 10, CellHeight: 20})
	if got, want := picture.RGBAAt(0, 10), (color.RGBA{R: 5, G: 6, B: 7, A: 0xff}); got != want {
		t.Errorf("track colour = %+v, want %+v", got, want)
	}
}

func drawnReport(t *testing.T, at time.Time) string {
	t.Helper()

	scoped := agent.UsageWindow{
		Duration: 5 * time.Hour,
		Percent:  8,
		ResetsAt: collectedAt.Add(2 * time.Hour),
		Scope:    "gpt-5.3-codex-spark",
	}

	report := Collect(t.Context(), []Source{
		{
			Provider: "codex",
			Label:    "OpenAI",
			Reporter: &scriptedReporter{windows: []agent.UsageWindow{sessionWindow(70), weeklyWindow(20), scoped}},
		},
		{
			Provider:             "anthropic",
			Label:                "Anthropic",
			Reporter:             &scriptedReporter{windows: []agent.UsageWindow{weeklyWindow(50)}},
			HasIdleSessionWindow: true,
		},
	}, nowAt(collectedAt))

	return style.Plain(Render(report, at, nil))
}

func TestEveryGaugeIsDrawnAgainstTheSameColumn(t *testing.T) {
	drawn := drawnReport(t, collectedAt.Add(time.Minute))

	want := strings.Join([]string{
		"● OpenAI",
		"Session        70% █████████┃█░░░░░ ▲ 10  1h 59m",
		"Week           20% ███░░░░░░░░┃░░░░ ▼ 51  1d 23h",
		"Spark Session   8% █░░░░░░░░┃░░░░░░ ▼ 52  1h 59m",
		"",
		"● Anthropic",
		"Session         0% ░░░░░░░░░░░░░░░░       idle",
		"Week           50% ████████░░░┃░░░░ ▼ 21  1d 23h",
		"",
	}, "\n")

	if drawn != want {
		t.Errorf("drew\n%s\nwant\n%s", drawn, want)
	}
}

func TestTheBulletFollowsTheFreshnessOfTheSnapshot(t *testing.T) {
	for _, test := range []struct {
		freshness        string
		age              time.Duration
		isSelfRefreshing bool
		mark             string
		style            style.Style
		isAgeWorthSayng  bool
	}{
		{freshness: FreshnessFresh, age: time.Minute, mark: freshMark, style: style.Success},
		{freshness: FreshnessDue, age: 10 * time.Minute, mark: freshMark, style: style.Change, isAgeWorthSayng: true},
		{
			freshness:        FreshnessStale,
			age:              45 * time.Minute,
			isSelfRefreshing: true,
			mark:             freshMark,
			style:            style.Failure,
			isAgeWorthSayng:  true,
		},
		{
			freshness:       FreshnessWaiting,
			age:             45 * time.Minute,
			mark:            waitingMark,
			style:           style.Change,
			isAgeWorthSayng: true,
		},
	} {
		t.Run(test.freshness, func(t *testing.T) {
			snapshot := Snapshot{
				FreshWithinSeconds: 360,
				StaleAfterSeconds:  1800,
				IsSelfRefreshing:   test.isSelfRefreshing,
			}

			if got := snapshot.FreshnessAt(test.age); got != test.freshness {
				t.Errorf("read the freshness as %q", got)
			}

			mark, appearance, isAgeWorthSaying := freshness(test.age, snapshot)
			if mark != test.mark {
				t.Errorf("drew the mark as %q", mark)
			}

			if got := appearance(mark); got != test.style(mark) {
				t.Errorf("drew the bullet as %q", got)
			}

			if isAgeWorthSaying != test.isAgeWorthSayng {
				t.Errorf("said the age: %t", isAgeWorthSaying)
			}
		})
	}
}

func TestAProviderThatCouldNotBeReachedSaysWhy(t *testing.T) {
	report := Report{Providers: []Snapshot{{
		Provider: Provider{ID: "anthropic", Label: "Anthropic"},
		Status:   StatusFailed,
		Message:  "refused with 429",
	}}}

	drawn := style.Plain(Render(report, collectedAt, nil))

	if drawn != "✖ Anthropic refused with 429\n" {
		t.Errorf("drew %q", drawn)
	}
}

func TestAnEmptyReportSaysThereIsNothingToShow(t *testing.T) {
	drawn := style.Plain(Render(Report{}, collectedAt, nil))

	if drawn != "no usage to report\n" {
		t.Errorf("drew %q", drawn)
	}
}

func TestACountdownLeadsWithItsLargestUnit(t *testing.T) {
	for _, test := range []struct {
		remainingTime time.Duration
		want          string
	}{
		{remainingTime: 45 * time.Second, want: "45s"},
		{remainingTime: 90 * time.Second, want: "1m 30s"},
		{remainingTime: 3 * time.Hour, want: "3h 0m"},
		{remainingTime: 75*time.Hour + 57*time.Minute, want: "3d 3h"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := style.Plain(countdown(test.remainingTime)); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestACountdownDimsTheUnitAfterItsFirst(t *testing.T) {
	drawn := countdown(75*time.Hour + 57*time.Minute)

	if want := style.Normal("3d") + " " + style.Dim("3h"); drawn != want {
		t.Errorf("drew %q, want %q", drawn, want)
	}
}

func TestADrawnGaugeTakesExactlyTheCellsItWasGiven(t *testing.T) {
	expected := 40
	limit := Limit{UsedPercent: 62, ExpectedPercent: &expected}

	gauges := FixedGauges(Graphics{CellWidth: 9, CellHeight: 18})

	drawn := gauges.Draw(limit.UsedPercent, limit.ExpectedPercent, PaceAhead, gaugeWidth)

	if got := width.Of(drawn); got != gaugeWidth {
		t.Errorf("the gauge occupies %d cells, want %d", got, gaugeWidth)
	}

	if got := style.Plain(drawn); width.Of(got) != gaugeWidth {
		t.Errorf("the placeholder run reads %q", got)
	}
}

func TestADrawnRowStillLinesUpWithTheRestOfTheReport(t *testing.T) {
	report := Collect(t.Context(), []Source{{
		Provider: "codex",
		Label:    "OpenAI",
		Reporter: &scriptedReporter{windows: []agent.UsageWindow{sessionWindow(70), weeklyWindow(20)}},
	}}, nowAt(collectedAt))

	gauges := FixedGauges(Graphics{CellWidth: 9, CellHeight: 18})

	for _, line := range strings.Split(strings.TrimSuffix(Render(report, collectedAt, gauges), "\n"), "\n")[1:] {
		if got := width.Of(line); got != width.Of(style.Plain(line)) {
			t.Errorf("the line %q measures %d", line, got)
		}
	}
}

func TestEveryDrawingCarriesThePictureItPlaces(t *testing.T) {
	expected := 40
	gauges := FixedGauges(Graphics{CellWidth: 9, CellHeight: 18})

	first := gauges.Draw(62, &expected, PaceAhead, gaugeWidth)
	second := gauges.Draw(62, &expected, PaceAhead, gaugeWidth)

	if !strings.Contains(first, "a=T") {
		t.Error("the first drawing did not transmit the picture")
	}

	if !strings.Contains(second, "a=T") {
		t.Error("the second drawing placed the picture without transmitting it")
	}

	if width.Of(second) != gaugeWidth {
		t.Errorf("the second drawing occupies %d cells, want %d", width.Of(second), gaugeWidth)
	}

	if changed := gauges.Draw(63, &expected, PaceAhead, gaugeWidth); !strings.Contains(changed, "a=T") {
		t.Error("a gauge that moved did not transmit a picture of its own")
	}
}

func TestOneImageIsHeldForEachGaugeHoweverOftenItIsDrawn(t *testing.T) {
	expected := 40
	gauges := FixedGauges(Graphics{CellWidth: 9, CellHeight: 18})

	identifiers := map[string]struct{}{}

	for range 3 {
		drawn := gauges.Draw(62, &expected, PaceAhead, gaugeWidth)

		matched := identifierPattern.FindString(drawn)
		if matched == "" {
			t.Fatalf("the drawing %q names no image", drawn)
		}

		identifiers[matched] = struct{}{}
	}

	if len(identifiers) != 1 {
		t.Errorf("one gauge drawn three times took %d image identifiers", len(identifiers))
	}
}

func TestThePlacementsHeldAreBounded(t *testing.T) {
	gauges := FixedGauges(Graphics{CellWidth: 9, CellHeight: 18})

	for usedPercent := range mostPlacements + 1 {
		if drawn := gauges.Draw(usedPercent, nil, PaceEven, gaugeWidth); !strings.Contains(drawn, "a=T") {
			t.Fatalf("the gauge at %d%% was drawn without its picture", usedPercent)
		}
	}

	if held := len(gauges.placements); held > mostPlacements {
		t.Errorf("%d placements are held, want no more than %d", held, mostPlacements)
	}
}
