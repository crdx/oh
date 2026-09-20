package input

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/app/edit"
	"crdx.org/io/internal/app/style"
)

func TestTheRuleIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 40, 100} {
		for _, label := range []string{"", "gpt ⠶ 6 tools ⠶ io", strings.Repeat("wide", 40)} {
			rule := Ruler{Right: label}
			if got := style.Width(rule.render(width, style.Rule)); got != width {
				t.Errorf("expected a rule of %d columns, got %d", width, got)
			}
		}
	}
}

func TestTheLabelSitsAtTheRightHandEndOfTheRule(t *testing.T) {
	rule := Ruler{Right: style.Subtle("here")}

	want := " " + style.Subtle("here") + " " + style.Rule("──")
	if got := rule.render(20, style.Rule); !strings.HasSuffix(got, want) {
		t.Errorf("expected %q to end in %q", got, want)
	}
}

func TestARuleWithBothLabelsIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 20, 40, 100} {
		rule := Ruler{Left: "↑ 12", Right: "gpt ⠶ io"}
		if got := style.Width(rule.render(width, style.Rule)); got != width {
			t.Errorf("expected a rule of %d columns, got %d", width, got)
		}
	}
}

func TestLeftContentRoomKeepsAFittingRightLabel(t *testing.T) {
	if got := LeftContentWidth(20, "right"); got != 7 {
		t.Errorf("got %d cells, want 7", got)
	}
	if got := LeftContentWidth(20, ""); got != 16 {
		t.Errorf("got %d cells without a right label, want 16", got)
	}
	if got := LeftContentWidth(5, "far too long"); got != 1 {
		t.Errorf("got %d cells beside an unfit right label, want 1", got)
	}
}

func TestTheLeftLabelIsDroppedFirstWhenTheRightIsKept(t *testing.T) {
	rule := Ruler{Left: "↑ 12", Right: "gpt ⠶ io"}

	got := rule.render(18, style.Rule)
	if strings.Contains(got, "12") {
		t.Errorf("expected the left label to be dropped, got %q", got)
	}
	if !strings.Contains(got, "gpt ⠶ io") {
		t.Errorf("expected the right label to be kept, got %q", got)
	}
}

func TestALabelTooWideForTheScreenIsDropped(t *testing.T) {
	rule := Ruler{Right: "far too long"}

	if got := rule.render(5, style.Rule); strings.Contains(got, "far") {
		t.Errorf("expected the label to be dropped, got %q", got)
	}
}

func TestTheBottomRuleCarriesALabelAtEitherEnd(t *testing.T) {
	block := Block{Bottom: Ruler{Left: "⠶ ─ io ─ gpt", Right: "↓ 2"}}

	got := style.Plain(bottomRuleOf(block, 40))
	if !strings.HasPrefix(got, "── ⠶ ─ io ─ gpt ") {
		t.Errorf("expected the label at the left, got %q", got)
	}
	if !strings.HasSuffix(got, " ↓ 2 ──") {
		t.Errorf("expected the scroll marker at the right, got %q", got)
	}
}

func TestTheBottomRuleDropsItsRightLabelToKeepItsLeftOne(t *testing.T) {
	block := Block{Bottom: Ruler{Left: "⠶ ─ io ─ gpt", Right: "↓ 200"}}

	got := style.Plain(bottomRuleOf(block, 20))
	if !strings.Contains(got, "⠶ ─ io ─ gpt") {
		t.Errorf("expected the left label to survive, got %q", got)
	}
	if strings.Contains(got, "200") {
		t.Errorf("expected the scroll marker to give way, got %q", got)
	}
}

func bottomRuleOf(block Block, width int) string {
	rows, _, _ := block.Rows(width)

	return rows[len(block.Status)+len(block.Input.Rows)+1]
}

func TestALabelPaintedDownToNothingCostsNothing(t *testing.T) {
	bare := Ruler{Right: "here"}
	painted := Ruler{Left: style.ScrolledInput(""), Right: "here"}

	if want, got := bare.render(20, style.Rule), painted.render(20, style.Rule); want != got {
		t.Errorf("expected an empty painted label to be ignored, got %q rather than %q", got, want)
	}
}

func TestTheBlockFramesTheInputBetweenItsRules(t *testing.T) {
	block := Block{
		Top:    Ruler{Left: "↑ 3"},
		Input:  edit.Frame{Rows: []string{"one", "two"}, Row: 1, Column: 2},
		Bottom: Ruler{Left: "⠶ ─ io"},
	}

	rows, cursorRow, cursorColumn := block.Rows(40)

	if len(rows) != 4 {
		t.Fatalf("expected a rule either side of two input rows, got %d rows", len(rows))
	}
	if rows[1] != "one" || rows[2] != "two" {
		t.Errorf("expected the input rows between the rules, got %q", rows)
	}
	if cursorRow != 2 {
		t.Errorf("expected the cursor a row below the top rule, got %d", cursorRow)
	}
	if cursorColumn != 2 {
		t.Errorf("expected the cursor column to be carried over, got %d", cursorColumn)
	}
}

func TestAHistorySearchLabelsTheLeftOfTheTopRule(t *testing.T) {
	block := Block{
		Top:   Ruler{Left: "ordinary"},
		Input: edit.Frame{Rows: []string{"git diff"}, IsSearching: true, SearchQuery: "git"},
	}

	rows, _, _ := block.Rows(40)
	if got := style.Plain(rows[0]); !strings.HasPrefix(got, "── reverse-i-search: git ") {
		t.Errorf("expected the search query at the left of the top rule, got %q", got)
	}
}

func TestStatusRowsSitAboveTheTopRuleWithoutMovingTheInput(t *testing.T) {
	block := Block{
		Input:  edit.Frame{Rows: []string{"input"}, Row: 0, Column: 3},
		Status: []string{"first status row", "second status row"},
	}

	rows, cursorRow, cursorColumn := block.Rows(40)

	if len(rows) != 5 || rows[0] != "first status row" || rows[1] != "second status row" {
		t.Errorf("expected status rows above the top rule, got %q", rows)
	}
	if cursorRow != 3 || cursorColumn != 3 {
		t.Errorf("status moved the cursor to %d,%d within the footer", cursorRow, cursorColumn)
	}
	if rowsFromBottom := len(rows) - cursorRow; rowsFromBottom != 2 {
		t.Errorf("status moved the input to %d rows from the bottom, want 2", rowsFromBottom)
	}
}

func TestFeedbackRowsFormABoxAttachedToTheTopRule(t *testing.T) {
	const columns = 20
	block := Block{
		Input:         edit.Frame{Rows: []string{"input"}, Row: 0, Column: 3},
		Status:        []string{"first", "second"},
		FrameFeedback: true,
	}

	rows, cursorRow, cursorColumn := block.Rows(columns)
	plainRows := make([]string, len(rows))
	for i, row := range rows {
		plainRows[i] = style.Plain(row)
		if i == 3 || i == 5 {
			if got := style.Width(row); got != columns {
				t.Errorf("row %d is %d columns wide, want %d: %q", i, got, columns, plainRows[i])
			}
		}
	}

	want := []string{
		" ╭────────────────╮",
		" │ first          │",
		" │ second         │",
		"─┴────────────────┴─",
		"input",
		"────────────────────",
	}
	if !slices.Equal(plainRows, want) {
		t.Errorf("got rows %q, want %q", plainRows, want)
	}
	if cursorRow != 4 || cursorColumn != 3 {
		t.Errorf("feedback moved the cursor to %d,%d within the footer", cursorRow, cursorColumn)
	}
	if rowsFromBottom := len(rows) - cursorRow; rowsFromBottom != 2 {
		t.Errorf("feedback moved the input to %d rows from the bottom, want 2", rowsFromBottom)
	}
}

func TestTheFeedbackFrameStandsInsideTheEndsOfTheRule(t *testing.T) {
	const columns = 20
	block := Block{
		Input:         edit.Frame{Rows: []string{""}},
		Status:        []string{"note"},
		FrameFeedback: true,
	}

	rows, _, _ := block.Rows(columns)
	box := []rune(style.Plain(rows[0]))
	join := []rune(style.Plain(rows[2]))

	if len(join) != columns {
		t.Fatalf("the joined rule is %d columns wide, want %d", len(join), columns)
	}
	for i := range feedbackInset {
		if join[i] != '─' || join[columns-1-i] != '─' {
			t.Errorf("the rule stops short of the edge: %q", string(join))
		}
	}
	if join[feedbackInset] != '┴' || join[columns-1-feedbackInset] != '┴' {
		t.Errorf("the frame does not stand on the rule: %q", string(join))
	}
	if box[feedbackInset] != '╭' || box[columns-1-feedbackInset] != '╮' {
		t.Errorf("the frame corners do not sit above the junctions: %q", string(box))
	}
}

func TestFeedbackFrameIsDroppedWhenItCannotHoldContent(t *testing.T) {
	for columns := range MinimumFramedFeedbackWidth {
		block := Block{
			Input:         edit.Frame{Rows: []string{""}},
			Status:        []string{"x"},
			FrameFeedback: true,
		}

		rows, _, _ := block.Rows(columns)
		if strings.ContainsAny(style.Plain(strings.Join(rows, "")), "╭╮│┴") {
			t.Errorf("feedback was framed in %d columns: %q", columns, rows)
		}
		if got := FeedbackContentWidth(columns); got != columns {
			t.Errorf("content width in %d columns is %d", columns, got)
		}
		if got := FeedbackRuleWidth(columns); got != columns {
			t.Errorf("rule width in %d columns is %d", columns, got)
		}
	}
}

func TestACentredLabelSitsInTheMiddleOfTheDashes(t *testing.T) {
	rule := Ruler{Center: "io"}

	got := style.Plain(rule.render(40, style.Rule))
	if want := strings.Repeat("─", 18) + " io " + strings.Repeat("─", 18); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestACentredLabelIsPositionedRelativeToTheRuleEdges(t *testing.T) {
	rule := Ruler{Left: "L", Center: "io", Right: "right side"}

	got := style.Plain(rule.render(40, style.Rule))
	beforeCenter, _, _ := strings.Cut(got, " io ")
	if want := (40 - len(" io ")) / 2; style.Width(beforeCenter) != want {
		t.Errorf("expected the centred label to start at column %d, got %q", want, got)
	}
}

func TestACentredLabelMeetingAnotherKeepsOneSpaceBetweenThem(t *testing.T) {
	rule := Ruler{Left: "left", Center: "mid", Right: "right"}

	if want, got := "── left mid ─ right ──", style.Plain(rule.render(22, style.Rule)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestACentredLabelIsNeverSetOffByAnUnevenGap(t *testing.T) {
	for width := range 121 {
		rule := Ruler{Left: "left", Center: "mid", Right: "right"}

		got := style.Plain(rule.render(width, style.Rule))
		if strings.Contains(got, "  mid") || strings.Contains(got, "mid  ") {
			t.Errorf("at %d columns the centred label has an uneven gap: %q", width, got)
		}
		if style.Width(got) != width {
			t.Errorf("at %d columns the rule is %d columns wide: %q", width, style.Width(got), got)
		}
	}
}

func TestARuleWithALabelAtEveryPlaceIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 5, 20, 21, 40, 100} {
		rule := Ruler{Left: "↑ 12", Center: "io", Right: "gpt ⠶ io"}
		if got := style.Width(rule.render(width, style.Rule)); got != width {
			t.Errorf("expected a rule of %d columns, got %d", width, got)
		}
	}
}

func TestTheCentredLabelIsTheFirstToGiveWay(t *testing.T) {
	rule := Ruler{Left: "↑ 12", Center: "workspace", Right: "gpt ⠶ io"}

	got := style.Plain(rule.render(25, style.Rule))
	if strings.Contains(got, "workspace") {
		t.Errorf("expected the centred label to give way, got %q", got)
	}
	if !strings.Contains(got, "↑ 12") || !strings.Contains(got, "gpt ⠶ io") {
		t.Errorf("expected the labels at either end to be kept, got %q", got)
	}
}

func TestACentredLabelIsKeptWhenThereIsRoomForItBetweenTheEnds(t *testing.T) {
	rule := Ruler{Left: "↑ 12", Center: "io", Right: "gpt"}

	got := style.Plain(rule.render(40, style.Rule))
	for _, want := range []string{"↑ 12", "io", "gpt"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive, got %q", want, got)
		}
	}
}

func TestACentredLabelGivesWayRatherThanMovingOffCentre(t *testing.T) {
	rule := Ruler{Left: "↑ 12", Center: "workspace", Right: "gpt ⠶ io"}

	got := style.Plain(rule.render(30, style.Rule))
	if strings.Contains(got, "workspace") {
		t.Errorf("expected the centred label to give way, got %q", got)
	}
}

func TestABlockDrawsItsRulesInTheStyleItWasGiven(t *testing.T) {
	build := func(rule style.Style) []string {
		block := Block{
			Top:    Ruler{Left: "left"},
			Input:  edit.Frame{Rows: []string{"> hello"}},
			Bottom: Ruler{Right: "right"},
			Rule:   rule,
		}

		rows, _, _ := block.Rows(40)
		return rows
	}

	given := build(style.Failure)
	byDefault := build(nil)

	for i, row := range given {
		if style.Plain(row) != style.Plain(byDefault[i]) {
			t.Errorf("row %d reads %q with a style and %q without one", i, style.Plain(row), style.Plain(byDefault[i]))
		}
	}

	if given[0] == byDefault[0] || given[2] == byDefault[2] {
		t.Error("expected the rules to be painted in the style the block was given")
	}

	if given[1] != byDefault[1] {
		t.Error("expected the input itself to be left alone")
	}
}

func TestAQuestionTakesTheRoomTheInputWouldHave(t *testing.T) {
	block := Block{
		Input:    edit.Frame{Rows: []string{"one", "two"}, Row: 1, Column: 3},
		Question: []string{"Continue?", "[y] yes"},
	}

	rows, cursorRow, cursorColumn := block.Rows(40)

	if len(rows) != 4 {
		t.Fatalf("got %d rows, want a rule, two question rows and a rule", len(rows))
	}
	if rows[1] != "Continue?" || rows[2] != "[y] yes" {
		t.Errorf("got body rows %q and %q", rows[1], rows[2])
	}
	if cursorRow != 2 {
		t.Errorf("got cursor row %d, want the last question row", cursorRow)
	}
	if cursorColumn != 0 {
		t.Errorf("got cursor column %d, want none while nobody may type", cursorColumn)
	}
}

func TestAQuestionKeepsTheSearchPromptAway(t *testing.T) {
	block := Block{
		Input:    edit.Frame{Rows: []string{"one"}, IsSearching: true, SearchQuery: "curl"},
		Question: []string{"Continue?"},
	}

	rows, _, _ := block.Rows(40)

	if strings.Contains(style.Plain(rows[0]), "reverse-i-search") {
		t.Errorf("a question was headed by the search prompt: %q", style.Plain(rows[0]))
	}
}
