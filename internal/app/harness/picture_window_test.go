package harness

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/graphics"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

const pictureApprovalLines = 40

func pictureArrivingDuringApproval(t *testing.T, isLocal bool) (string, string) {
	t.Helper()

	directory, reference := storedPictureFor(t, 800, 850)
	var drawn strings.Builder
	screen := output.NewTerminalOfSize(&drawn, replayColumns, pictureApprovalLines)
	picasso := newTestPainter(screen, false)
	picasso.DrawPicturesFrom(goldenPictureDisplay(directory, isLocal))
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)
	picasso.DrawEvent(pictureCallEvents(reference)[0])
	for _, identifier := range []string{"fetch-1", "fetch-2"} {
		picasso.DrawEvent(agent.Event{
			Kind: agent.ToolCallRequestEvent, ID: identifier, Name: "fetch",
			Arguments: `{"url":"https://example.com"}`,
		})
	}

	screen.InertFooter(
		[]string{"─", "Fetch this page?", "", "https://example.com", "", "[Yes]  No", "─"},
		5, output.Pins{Head: 2, Tail: 2}, 0,
	)
	before := drawn.String()
	picasso.DrawEvent(pictureCallEvents(reference)[1])

	return before, drawn.String()
}

func TestGoldenAPictureArrivingDuringAnApproval(t *testing.T) {
	passes := map[string]func() string{}
	screenPasses := map[string]func() string{}

	for _, format := range []struct {
		name    string
		isLocal bool
	}{
		{name: "PNG data", isLocal: false},
		{name: "PNG path", isLocal: true},
	} {
		before, after := pictureArrivingDuringApproval(t, format.isLocal)
		if strings.Contains(before, "f=100") {
			t.Errorf("%s: image was transmitted before its read result", format.name)
		}
		if count := strings.Count(after, "f=100"); count != 1 {
			t.Errorf("%s: image was transmitted %d times while its first row was hidden, want once", format.name, count)
		}

		requireNothingDrawnAboveTheScreen(t, format.name+" before read result", before, pictureApprovalLines)
		requireNothingDrawnAboveTheScreen(t, format.name+" after read result", after, pictureApprovalLines)
		passes[format.name+" before read result"] = func() string { return anonymisePictures(before) }
		passes[format.name+" after read result"] = func() string { return anonymisePictures(after) }
		screenPasses[format.name+" before read result"] = func() string {
			return shownInLines(t, before, pictureApprovalLines)
		}
		screenPasses[format.name+" after read result"] = func() string {
			return shownInLines(t, after, pictureApprovalLines)
		}
	}

	addPass := func(name string, stream string, lines int) {
		requireNothingDrawnAboveTheScreen(t, name, stream, lines)
		passes[name] = func() string { return anonymisePictures(stream) }
		screenPasses[name] = func() string { return shownInLines(t, stream, lines) }
	}

	before, during, after, completed, replayed := pictureAlreadyDrawnWhenApprovalOpens(t)
	if count := strings.Count(before, "f=100"); count != 1 {
		t.Errorf("picture before approval transmitted %d times, want once", count)
	}
	if count := strings.Count(during, "f=100"); count != 2 {
		t.Errorf("picture under approval transmitted %d times, want twice as its first row moved out", count)
	}
	addPass("picture already drawn before approval", before, pictureApprovalLines)
	addPass("picture drawn as approval opens", during, pictureApprovalLines)
	if count := strings.Count(after, "f=100"); count != 3 {
		t.Errorf("picture after approval closed transmitted %d times, want three", count)
	}
	addPass("picture after approval closes", after, pictureApprovalLines)
	addPass("picture after calls finish", completed, pictureApprovalLines)
	requireSameVisibleScreenOfSize(t, "a picture clipped by an approval changed on replay", replayColumns, pictureApprovalLines, completed, replayed)

	oneRow := pictureWithOnlyOneRowVisible(t, true)
	if count := strings.Count(oneRow, "f=100"); count != 1 {
		t.Errorf("single visible row transmitted %d times, want once", count)
	}
	addPass("last picture row alone", oneRow, 9)

	hidden := pictureWithOnlyOneRowVisible(t, false)
	if strings.Contains(hidden, "f=100") {
		t.Error("entirely hidden picture was transmitted")
	}
	addPass("picture entirely hidden", hidden, 9)

	compareWithGolden(t, "picture-approval", ".ansi", passes)
	compareWithGolden(t, "picture-approval", ".screen", screenPasses)
}

func pictureAlreadyDrawnWhenApprovalOpens(t *testing.T) (string, string, string, string, string) {
	t.Helper()

	directory, reference := storedPictureFor(t, 800, 600)
	var drawn strings.Builder
	screen := output.NewTerminalOfSize(&drawn, replayColumns, pictureApprovalLines)
	picasso := newTestPainter(screen, false)
	picasso.DrawPicturesFrom(goldenPictureDisplay(directory, false))
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)
	picasso.DrawEvent(pictureCallEvents(reference)[0])
	picasso.DrawEvent(agent.Event{
		Kind: agent.ToolCallRequestEvent, ID: "fetch", Name: "fetch",
		Arguments: `{"url":"https://example.com"}`,
	})
	picasso.DrawEvent(pictureCallEvents(reference)[1])
	before := drawn.String()

	screen.InertFooter(
		[]string{"─", "Fetch this page?", "", "https://example.com", "detail 1", "detail 2", "detail 3", "detail 4", "detail 5", "", "[Yes]  No", "─"},
		10, output.Pins{Head: 2, Tail: 2}, 0,
	)
	underApproval := drawn.String()
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)
	afterApproval := drawn.String()
	picasso.DrawEvent(agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "fetch", Name: "fetch", Text: "the page was read",
		Status: agent.SuccessStatus,
	})
	screen.Seal()
	completed := drawn.String()

	var replayed strings.Builder
	replayScreen := output.NewTerminalOfSize(&replayed, replayColumns, pictureApprovalLines)
	replayPainter := newTestPainter(replayScreen, false)
	replayPainter.DrawPicturesFrom(goldenPictureDisplay(directory, false))
	replayScreen.Footer([]string{"─", "> ", "─"}, 1, 2)
	for _, event := range []agent.Event{
		pictureCallEvents(reference)[0],
		{Kind: agent.ToolCallRequestEvent, ID: "fetch", Name: "fetch", Arguments: `{"url":"https://example.com"}`},
		pictureCallEvents(reference)[1],
		{Kind: agent.ToolCallResultEvent, ID: "fetch", Name: "fetch", Text: "the page was read", Status: agent.SuccessStatus},
	} {
		replayPainter.DrawEvent(event)
	}
	replayScreen.Seal()

	return before, underApproval, afterApproval, completed, replayed.String()
}

type pictureWindowBlock struct {
	rows []string
}

func (self pictureWindowBlock) Rows(int) []string { return self.rows }

func pictureWithOnlyOneRowVisible(t *testing.T, isPictureVisible bool) string {
	t.Helper()

	var drawn strings.Builder
	screen := output.NewTerminalOfSize(&drawn, replayColumns, 9)
	screen.InertFooter(
		[]string{"─", "Fetch this page?", "", "https://example.com", "", "[Yes]  No", "─"},
		5, output.Pins{Head: 2, Tail: 2}, 0,
	)
	pictureRows, isPlaced := graphics.PlacePNG(drawnPNGFor(t, 400, 200), graphics.Box{Cells: 40, Rows: 10})
	if !isPlaced {
		t.Fatal("picture was not placed")
	}
	rows := append([]string{"read"}, pictureRows...)
	if !isPictureVisible {
		rows = append(rows, "fetch")
	}
	screen.OpenTool(pictureWindowBlock{rows: rows})

	return drawn.String()
}
