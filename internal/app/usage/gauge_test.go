package usage

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/app/ptytest"
)

const (
	graphicsAnswer = "\x1b_Gi=1;OK\x1b\\\x1b[?62;4c"
	deviceAnswer   = "\x1b[?62;4c"
)

func answeringTerminal(t *testing.T, reply string) *os.File {
	t.Helper()

	controller, terminal := ptytest.OpenSized(t, 80, 24)
	go func() {
		asked := make([]byte, 256)
		if _, err := controller.Read(asked); err != nil {
			return
		}
		_, _ = controller.WriteString(reply)
	}()

	return terminal
}

func TestATerminalWithoutGraphicsGetsBlockGauges(t *testing.T) {
	terminal := answeringTerminal(t, deviceAnswer)
	expected := 40

	gauges := TerminalGauges(terminal, terminal)

	if gauges.measure != nil {
		t.Fatal("a terminal that ignored the graphics probe was measured for pictures")
	}
	drawn := gauges.Draw(62, &expected, PaceAhead, 12)
	if want := blockGauge(62, &expected, PaceAhead, 12); drawn != want {
		t.Errorf("got %q, want the block gauge %q", drawn, want)
	}
}

func TestATerminalWithGraphicsGetsPictureGaugesSizedFromItsCells(t *testing.T) {
	terminal := answeringTerminal(t, graphicsAnswer)
	expected := 40

	gauges := TerminalGauges(terminal, terminal)

	drawing, hasGraphics := gauges.measure()
	if !hasGraphics {
		t.Fatal("a terminal that answered the graphics probe was not measured for pictures")
	}
	if drawing.CellWidth != 10 || drawing.CellHeight != 20 {
		t.Errorf("got %dx%d cells from a terminal naming no pixels, want the 10x20 defaults",
			drawing.CellWidth, drawing.CellHeight)
	}
	drawn := gauges.Draw(62, &expected, PaceAhead, 12)
	if drawn == blockGauge(62, &expected, PaceAhead, 12) {
		t.Error("a terminal with graphics was drawn a block gauge")
	}

	size := &unix.Winsize{Row: 24, Col: 80, Xpixel: 720, Ypixel: 432}
	if err := unix.IoctlSetWinsize(int(terminal.Fd()), unix.TIOCSWINSZ, size); err != nil {
		t.Fatal(err)
	}
	drawing, _ = gauges.measure()
	if drawing.CellWidth != 9 || drawing.CellHeight != 18 {
		t.Errorf("got %dx%d cells after the terminal named its pixels, want 9x18",
			drawing.CellWidth, drawing.CellHeight)
	}
}

func TestGaugesForAnAnswerAlreadyGivenMatchTheTerminalsOwn(t *testing.T) {
	expected := 40
	for _, reply := range []string{graphicsAnswer, deviceAnswer} {
		terminal := answeringTerminal(t, reply)
		asked := TerminalGauges(terminal, terminal)
		given := GaugesFor(terminal, 10, 20, reply == graphicsAnswer)

		if (asked.measure == nil) != (given.measure == nil) {
			t.Fatalf("for %q the terminal's gauges and the given gauges disagree on graphics", reply)
		}
		if asked.measure != nil {
			askedDrawing, _ := asked.measure()
			givenDrawing, _ := given.measure()
			if askedDrawing != givenDrawing {
				t.Errorf("for %q got %v from the terminal and %v from the answer", reply, askedDrawing, givenDrawing)
			}
		}
		block := blockGauge(62, &expected, PaceAhead, 12)
		isAskedInBlocks := asked.Draw(62, &expected, PaceAhead, 12) == block
		isGivenInBlocks := given.Draw(62, &expected, PaceAhead, 12) == block
		if isAskedInBlocks != isGivenInBlocks {
			t.Errorf("for %q one gauge drew blocks and the other a picture", reply)
		}
	}
}
