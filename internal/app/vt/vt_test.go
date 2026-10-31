package vt_test

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/vt"
)

const testColumns = 10

func play(t *testing.T, stream string, lines int) *vt.Screen {
	t.Helper()

	screen := vt.New(testColumns, lines)
	if err := screen.Play(stream); err != nil {
		t.Fatal(err)
	}

	return screen
}

func TestACursorPositionMovesToTheRowAndColumnGiven(t *testing.T) {
	drawn := play(t, "abc\r\ndef\x1b[1;2Hz\x1b[2;3Hy", 0)

	if got, want := drawn.Text(), []string{"azc", "dey"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestACursorPositionWithoutArgumentsGoesHome(t *testing.T) {
	drawn := play(t, "abc\x1b[Hz", 0)

	if got, want := drawn.Text(), []string{"zbc"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestQueriesAndModesThatDrawNothingAreAccepted(t *testing.T) {
	for _, sequence := range []string{
		"\x1b[c",
		"\x1b[>c",
		"\x1b[6n",
		"\x1b[>1u",
		"\x1b[<u",
		"\x1b[?u",
		"\x1b[5 q",
		"\x1b[22;0t",
		"\x1b[?1004h",
		"\x1b[?2004l",
		"\x1b[?5522h",
		"\x1b[?2031h",
	} {
		drawn := play(t, "x"+sequence+"y", 0)

		if got, want := drawn.Text(), []string{"xy"}; !slices.Equal(got, want) {
			t.Errorf("%q drew %q, want %q", sequence, got, want)
		}
	}
}

func TestAnUnknownSequenceStopsThePlayWithAnError(t *testing.T) {
	screen := vt.New(10, 0)

	err := screen.Play("a\x1b[5zb")
	if err == nil || !strings.Contains(err.Error(), "ESC [ 5z") {
		t.Fatalf("got %v, want an error naming the sequence", err)
	}
	if got, want := screen.Text(), []string{"a"}; !slices.Equal(got, want) {
		t.Errorf("drew %q past the error, want %q", got, want)
	}
}

func TestStyledOnScreenKeepsOnlyTheRowsAScreenShows(t *testing.T) {
	drawn := play(t, "one\r\ntwo\r\nthree\r\nfour", 2)

	if got, want := drawn.StyledOnScreen(), []string{"three", "four"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEachRegionIsMarkedWhereItBeginsAndEnds(t *testing.T) {
	names := map[int]string{1: "user", 2: "answer"}

	got := vt.WithRegions(
		[]string{"question", "", "more", "reply"},
		[]int{1, 0, 1, 2},
		func(owner int) string { return names[owner] },
	)

	want := vt.RegionStart("user") + "question\n\nmore" + vt.RegionEnd + "\n" +
		vt.RegionStart("answer") + "reply" + vt.RegionEnd + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRegionMarksDrawNothing(t *testing.T) {
	drawing := vt.WithRegions([]string{"a", "b"}, []int{1, 1}, func(int) string { return "user" })

	if got, want := play(t, strings.ReplaceAll(drawing, "\n", "\r\n"), 0).Text(), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
