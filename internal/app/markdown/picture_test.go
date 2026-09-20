package markdown

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
)

type recordingDrawer struct {
	rows     []string
	requests []string
	isDrawn  bool
}

func (self *recordingDrawer) DrawPicture(path string, columns int) ([]string, bool) {
	self.requests = append(self.requests, path)

	if !self.isDrawn || columns <= 0 {
		return nil, false
	}

	return self.rows, true
}

func newDrawer() *recordingDrawer {
	return &recordingDrawer{rows: []string{"<picture row 1>", "<picture row 2>"}, isDrawn: true}
}

func renderWithDrawer(markdown string, drawer PictureDrawer) []string {
	return RenderWith(markdown, Options{Columns: 40, Pictures: drawer})
}

func TestAPictureOfItsOwnIsDrawnWhereItIsNamed(t *testing.T) {
	drawer := newDrawer()

	rows := renderWithDrawer("before\n\n![](/pictures/chart.png)\n\nafter", drawer)

	want := []string{"before", "", "<picture row 1>", "<picture row 2>", "", "after"}
	if !slices.Equal(rows, want) {
		t.Errorf("drew %q, want %q", rows, want)
	}
	if !slices.Equal(drawer.requests, []string{"/pictures/chart.png"}) {
		t.Errorf("asked for %q, want the named path alone", drawer.requests)
	}
}

func TestAPictureThatCannotBeDrawnKeepsItsWrittenForm(t *testing.T) {
	drawer := newDrawer()
	drawer.isDrawn = false

	rows := renderWithDrawer("![a chart](/pictures/chart.png)", drawer)

	joined := strings.Join(rows, "\n")
	if !strings.Contains(style.Plain(joined), "/pictures/chart.png") {
		t.Errorf("drew %q, want the path written out", joined)
	}
}

func TestAPictureIsLeftAloneWhereNoDrawerIsGiven(t *testing.T) {
	rows := RenderWith("![a chart](/pictures/chart.png)", Options{Columns: 40})

	if !strings.Contains(style.Plain(strings.Join(rows, "\n")), "/pictures/chart.png") {
		t.Errorf("drew %q, want the path written out", rows)
	}
}

func TestAPictureAmongProseIsNotDrawn(t *testing.T) {
	drawer := newDrawer()

	rows := renderWithDrawer("look at ![](/pictures/chart.png) closely", drawer)

	if slices.Contains(rows, "<picture row 1>") {
		t.Error("a picture sharing its paragraph with prose was drawn")
	}
	if len(drawer.requests) != 0 {
		t.Errorf("asked for %q, want nothing", drawer.requests)
	}
}

func TestTwoPicturesInOneParagraphAreNotDrawn(t *testing.T) {
	drawer := newDrawer()

	rows := renderWithDrawer("![](/pictures/one.png) ![](/pictures/two.png)", drawer)

	if slices.Contains(rows, "<picture row 1>") {
		t.Error("a paragraph of two pictures was drawn as one")
	}
}

func TestAPictureIsStillDrawnWhereItsAlternativeWordsAreWritten(t *testing.T) {
	drawer := newDrawer()

	rows := renderWithDrawer("![a chart of the results](/pictures/chart.png)", drawer)

	want := []string{"<picture row 1>", "<picture row 2>"}
	if !slices.Equal(rows, want) {
		t.Errorf("drew %q, want %q", rows, want)
	}
}

func TestAPictureInAListIsDrawnBesideItsMarker(t *testing.T) {
	drawer := newDrawer()

	rows := renderWithDrawer("- ![](/pictures/chart.png)", drawer)

	if len(rows) != 2 || !strings.HasSuffix(rows[0], "<picture row 1>") {
		t.Errorf("drew %q, want the picture under the bullet", rows)
	}
}

func TestAnIncrementalAnswerDrawsAPictureOnlyOnceItIsWhole(t *testing.T) {
	const source = "here it is:\n\n![](/pictures/chart.png)\n\ndone"

	drawer := newDrawer()
	var renderer IncrementalRenderer
	options := Options{Columns: 40, Pictures: drawer}

	var rows []string
	for at := 1; at <= len(source); at++ {
		rows = renderer.RenderWith(source[:at], options)
		if slices.Contains(rows, "<picture row 1>") && at < strings.Index(source, ")")+1 {
			t.Fatalf("byte %d drew a picture before its address was whole", at)
		}
	}

	if !slices.Contains(rows, "<picture row 1>") {
		t.Errorf("drew %q, want the picture", rows)
	}
	if !slices.Equal(rows, RenderWith(source, options)) {
		t.Errorf("streamed %q, want the same as %q", rows, RenderWith(source, options))
	}
}
