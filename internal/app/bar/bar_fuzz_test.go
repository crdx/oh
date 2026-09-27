package bar

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

const (
	fuzzedSegmentSeparator = "\n"
	fuzzedRungSeparator    = "|"
	fuzzedLadderLength     = 512
	escape                 = '\x1b'
)

func fuzzedLayout(written string) (segment.Layout, [][]string) {
	var instances []segment.Segment
	var ladders [][]string

	for rungs := range strings.SplitSeq(written, fuzzedSegmentSeparator) {
		ladder := strings.Split(rungs, fuzzedRungSeparator)
		instances = append(instances, fittingSegment(ladder))
		ladders = append(ladders, ladder)
	}

	return segment.Layout{segment.TopLeft: instances}, ladders
}

func addedWidth(drawn string) int {
	if drawn == "" {
		return 0
	}

	texts := strings.Split(drawn, segmentSeparator())
	added := (len(texts) - 1) * style.Width(segmentSeparator())

	for _, text := range texts {
		added += style.Width(text)
	}

	return added
}

func FuzzWhatIsDrawnNeverOutgrowsTheRoomItWasGiven(fuzzer *testing.F) {
	for _, seed := range []string{
		"alpha|al|",
		"alpha|al|\nbravo|br|",
		"Opus 5 ·▫▫▪▫▫|O5 ·▫▫▪▫▫|O5|\n6% 62K/1M|6% 62K|6%|",
		"whole\nwhole\nwhole",
		"|",
		"",
		"\n\n\n",
		"a|aa|aaa|a",
		"日本語|日|",
		"a\u0301b|a|",
		"👨‍👩‍👧‍👦|👨|",
		"🇬🇧 ports|🇬🇧|",
		"☺\ufe0f|☺|",
		"한국어 글자|한|",
		"مرحبا|م|",
		"नमस्ते|न|",
		"\u200d|\u0301|",
		"ＧＰＴ|Ｇ|",
		"e\u0301|e|",
		"x\u0301|\u0301|",
		strings.Repeat("wide", 40) + "|+|",
	} {
		fuzzer.Add(seed, byte(20))
	}

	fuzzer.Fuzz(func(t *testing.T, written string, cells byte) {
		if len(written) > fuzzedLadderLength {
			t.Skip("longer than a bar is ever drawn")
		}

		if strings.ContainsRune(written, escape) {
			t.Skip("no segment writes an escape sequence of its own")
		}

		layout, ladders := fuzzedLayout(written)
		requireEverySegmentFits(t, layout, ladders, int(cells))

		styledLayout, styledLadders := fuzzedLayout(written)
		for at, ladder := range styledLadders {
			styled := make([]string, 0, len(ladder))
			for _, rung := range ladder {
				styled = append(styled, style.Subtle(rung))
			}

			styledLadders[at] = styled
			styledLayout[segment.TopLeft][at] = fittingSegment(styled)
		}

		requireEverySegmentFits(t, styledLayout, styledLadders, int(cells))
	})
}

func requireEverySegmentFits(t *testing.T, layout segment.Layout, ladders [][]string, room int) {
	t.Helper()

	context := segment.Context{}
	drawn := RenderWithin(layout, segment.TopLeft, context, room)

	if width := style.Width(drawn); width > room {
		t.Fatalf("%d cells drew %q, %d cells wide", room, drawn, width)
	}

	if again := RenderWithin(layout, segment.TopLeft, context, room); again != drawn {
		t.Fatalf("drawing twice in %d cells gave %q and then %q", room, drawn, again)
	}

	for text := range strings.SplitSeq(drawn, segmentSeparator()) {
		if text == "" {
			continue
		}

		if !slices.ContainsFunc(ladders, func(ladder []string) bool {
			return slices.Contains(ladder, text)
		}) {
			t.Fatalf("%q was drawn, which is nobody's rung", text)
		}
	}

	whole := Render(layout, segment.TopLeft, context)
	if room >= addedWidth(whole) && drawn != whole {
		t.Fatalf("%d cells hold the whole bar %q, but drew %q", room, whole, drawn)
	}
}
