package harness

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/pkg/agent"
)

type childWindowCase struct {
	name        string
	calls       int
	reports     int
	reportLines int
	extraNotice bool
	grouping    []string
}

var childWindowCases = []childWindowCase{
	{name: "one call and long report", calls: 1, reports: 1, reportLines: 40},
	{name: "three calls and long report", calls: 3, reports: 1, reportLines: 40},
	{name: "three calls and two reports", calls: 3, reports: 2, reportLines: 18},
	{name: "one call and short report", calls: 1, reports: 1, reportLines: 1},
	{name: "one call report and notice", calls: 1, reports: 1, reportLines: 40, extraNotice: true},
	{name: "only one long report", reports: 1, reportLines: 40},
	{name: "only three calls", calls: 3},
	{name: "grouped panel and tool", calls: 1, reports: 1, reportLines: 40, grouping: []string{"panel tool", "notice", "reasoning", "answer"}},
}

var (
	childWindowWidths  = []int{oneColumn, tinyColumns, narrowColumns, replayColumns}
	childWindowHeights = []int{2, 3, 4, 5, shortLines, 7, 8, 12, replayLines}
)

func TestGoldenEveryLiveChildHasAWindow(t *testing.T) {
	screenPasses := map[string]func() string{}
	ansiPasses := map[string]func() string{}

	for _, scene := range childWindowCases {
		for _, columns := range childWindowWidths {
			for _, lines := range childWindowHeights {
				name := fmt.Sprintf("%s at %d columns by %d lines", scene.name, columns, lines)
				screenPasses[name] = func() string {
					live, settled := drawChildWindowCase(t, scene, columns, lines)
					requireNothingDrawnAboveTheScreen(t, name, live, lines)
					_, roomy := drawChildWindowCase(t, scene, columns, 80)
					requireSameVisibleScreenInColumns(t, name+" sealed", columns, roomy, settled)
					return strings.Join(playScreenOfSize(t, live, columns, lines).text(), "\n")
				}
			}
		}
	}

	for _, scene := range childWindowCases {
		if scene.name == "one call and long report" || scene.name == "three calls and two reports" ||
			scene.name == "only one long report" || scene.name == "grouped panel and tool" {
			name := scene.name + " at 40 columns by 12 lines"
			ansiPasses[name] = func() string {
				live, _ := drawChildWindowCase(t, scene, 40, 12)
				return live
			}
		}
	}

	for paletteName, background := range map[string]style.Background{
		"dark": style.DarkBackground, "grey-dark": style.GreyDarkBackground,
		"grey-light": style.GreyLightBackground, "light": style.LightBackground,
	} {
		ansiPasses["panel elision on "+paletteName] = func() string {
			restoreTheme := style.ApplyTheme(style.DefaultTheme())
			defer restoreTheme()
			restoreBackground := style.ReportBackground(background)
			defer restoreBackground()
			live, _ := drawChildWindowCase(t, childWindowCases[0], narrowColumns, 12)
			return live
		}
	}

	compareWithGolden(t, "live-child-windows", ".screen", screenPasses)
	compareWithGolden(t, "live-child-windows", ".ansi", ansiPasses)
}

func drawChildWindowCase(t *testing.T, scene childWindowCase, columns int, lines int) (string, string) {
	t.Helper()
	var live, settled string
	synctest.Test(t, func(t *testing.T) {
		var drawn bytes.Buffer
		screen := output.NewTerminalOfSize(&drawn, columns, lines)
		if scene.grouping != nil {
			grouping, err := output.ParseGrouping(scene.grouping)
			if err != nil {
				t.Fatal(err)
			}
			screen.SetGrouping(grouping)
		}
		paint := painter.New(screen, true, nil, nil, output.StreamingModeLine)
		for index := range scene.calls {
			paint.DrawEvent(agent.Event{
				Kind: agent.ToolCallRequestEvent,
				ID:   fmt.Sprintf("call-%d", index), Name: "lookup",
				FallbackRendering: agent.FallbackRendering{Subject: fmt.Sprintf("query %d", index)},
			})
		}
		if scene.reports > 0 {
			reports := make([]subagentrecord.Report, scene.reports)
			names := make([]string, scene.reports)
			for index := range reports {
				names[index] = fmt.Sprintf("tame-adder-%d", index)
				reports[index] = subagentrecord.Report{
					Name: names[index], State: subagentrecord.Done,
					Answer: fmt.Sprintf("Report %d\n", index) + strings.Repeat("A line of verified detail.\n", scene.reportLines),
				}
			}
			paint.DrawEvent(subagentrecord.DeliveryEvent(names, "report", reports))
		}
		if scene.extraNotice {
			screen.Line("a separate harness notice")
		}
		screen.Footer([]string{"> "}, 0, 2)
		live = drawn.String()

		for index := range scene.calls {
			paint.DrawEvent(agent.Event{
				Kind: agent.ToolCallResultEvent,
				ID:   fmt.Sprintf("call-%d", index), Name: "lookup",
				Status: agent.SuccessStatus, Took: time.Second,
			})
		}
		screen.Seal()
		screen.End()
		screen.Release(true)
		settled = drawn.String()
	})
	return live, settled
}

func TestGoldenPicturesBesideLiveReports(t *testing.T) {
	screenPasses := map[string]func() string{}
	ansiPasses := map[string]func() string{}
	for _, columns := range []int{tinyColumns, narrowColumns, replayColumns} {
		for _, lines := range []int{shortLines, 12, replayLines} {
			name := fmt.Sprintf("a picture between calls beside a report at %d columns by %d lines", columns, lines)
			screenPasses[name] = func() string {
				live, settled := drawPictureBesideReport(t, columns, lines)
				requireNothingDrawnAboveTheScreen(t, name, live, lines)
				_, roomy := drawPictureBesideReport(t, columns, 80)
				requireSameVisibleScreenInColumns(t, name+" sealed", columns, anonymisePictures(roomy), anonymisePictures(settled))
				return strings.Join(playScreenOfSize(t, anonymisePictures(live), columns, lines).text(), "\n")
			}
			if columns == narrowColumns && lines == 12 || columns == replayColumns && lines == replayLines {
				ansiPasses[name] = func() string {
					live, _ := drawPictureBesideReport(t, columns, lines)
					return live
				}
			}
		}
	}
	compareWithGolden(t, "live-child-pictures", ".screen", screenPasses)
	compareWithGolden(t, "live-child-pictures", ".ansi", ansiPasses)
}

func drawPictureBesideReport(t *testing.T, columns int, lines int) (string, string) {
	t.Helper()
	sessionDirectory, reference := storedPictureFor(t, 400, 200)
	var live, settled string
	synctest.Test(t, func(t *testing.T) {
		var drawn bytes.Buffer
		screen := output.NewTerminalOfSize(&drawn, columns, lines)
		paint := painter.New(screen, true, nil, nil, output.StreamingModeLine)
		paint.DrawPicturesFrom(goldenPictureDisplay(sessionDirectory, false))
		paint.DrawEvent(agent.Event{
			Kind: agent.ToolCallRequestEvent, ID: "picture", Name: "read",
			FallbackRendering: agent.FallbackRendering{Subject: "screenshot.png"},
		})
		paint.DrawEvent(agent.Event{
			Kind: agent.ToolCallRequestEvent, ID: "lookup", Name: "lookup",
			FallbackRendering: agent.FallbackRendering{Subject: "RFC 8620"},
		})
		paint.DrawEvent(agent.Event{
			Kind: agent.ToolCallResultEvent, ID: "picture", Name: "read",
			Picture: reference, Status: agent.SuccessStatus,
		})
		paint.DrawEvent(subagentrecord.DeliveryEvent([]string{"tame-adder"}, "report", []subagentrecord.Report{{
			Name: "tame-adder", State: subagentrecord.Done,
			Answer: strings.Repeat("A line of verified detail.\n", 30),
		}}))
		screen.Footer([]string{"> "}, 0, 2)
		live = drawn.String()
		for range 3 {
			screen.Refresh()
		}
		if got := strings.Count(drawn.String(), "f=100"); got != strings.Count(live, "f=100") {
			t.Errorf("an unchanged frame retransmitted the picture: %d transmissions", got)
		}

		paint.DrawEvent(agent.Event{
			Kind: agent.ToolCallResultEvent, ID: "lookup", Name: "lookup",
			Status: agent.SuccessStatus, Took: time.Second,
		})
		screen.End()
		screen.Release(true)
		settled = drawn.String()
	})
	return live, settled
}
