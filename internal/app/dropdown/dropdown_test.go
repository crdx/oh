package dropdown_test

import (
	"fmt"
	"reflect"
	"testing"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/style"
)

var (
	tab      = key.Key{Code: key.Rune, Value: '\t'}
	shiftTab = key.Key{Code: key.Rune, Value: '\t', Mod: key.Shift}
	down     = key.Key{Code: key.Down}
	up       = key.Key{Code: key.Up}
	enter    = key.Key{Code: key.Enter}
	escape   = key.Key{Code: key.Escape}
)

func opened(options []dropdown.Option, total int) *dropdown.Dropdown {
	menu := &dropdown.Dropdown{}
	menu.Open()
	menu.SetPlaceholder("nothing here")
	menu.SetOptions(options, total)
	return menu
}

func numbered(count int) []dropdown.Option {
	options := make([]dropdown.Option, count)
	for i := range options {
		options[i] = dropdown.Option{Label: fmt.Sprintf("option %d", i+1)}
	}
	return options
}

func plainRows(menu *dropdown.Dropdown, columns int) []string {
	rows := menu.Rows(columns, dropdown.MaxRows)
	for i, row := range rows {
		rows[i] = style.Plain(row)
	}
	return rows
}

func TestAClosedDropdownDrawsNothingAndIgnoresKeys(t *testing.T) {
	var menu dropdown.Dropdown
	menu.SetOptions([]dropdown.Option{{Label: "one"}}, 1)

	if rows := menu.Rows(40, dropdown.MaxRows); rows != nil {
		t.Errorf("drew %q", rows)
	}
	if outcome := menu.Apply(enter); outcome != dropdown.Ignored {
		t.Errorf("enter gave %v", outcome)
	}
	if _, isSelected := menu.Selected(); isSelected {
		t.Error("a closed dropdown has a selection")
	}
}

func TestKeysMoveTheSelectionAroundTheOptions(t *testing.T) {
	menu := opened(numbered(3), 3)

	for _, step := range []struct {
		keypress key.Key
		want     int
	}{
		{down, 1},
		{down, 2},
		{down, 0},
		{up, 2},
		{up, 1},
	} {
		if outcome := menu.Apply(step.keypress); outcome != dropdown.Moved {
			t.Fatalf("%+v gave %v", step.keypress, outcome)
		}
		if got, _ := menu.Selected(); got != step.want {
			t.Errorf("%+v selected %d, want %d", step.keypress, got, step.want)
		}
	}
}

func TestTabChoosesTheSelection(t *testing.T) {
	menu := opened(numbered(2), 2)
	menu.Apply(down)

	if outcome := menu.Apply(tab); outcome != dropdown.Chosen {
		t.Errorf("tab gave %v", outcome)
	}
	if got, _ := menu.Selected(); got != 1 {
		t.Errorf("tab chose %d", got)
	}
	if outcome := menu.Apply(shiftTab); outcome != dropdown.Swallowed {
		t.Errorf("shift-tab gave %v", outcome)
	}
	if got, _ := menu.Selected(); got != 1 {
		t.Errorf("shift-tab moved the selection to %d", got)
	}
}

func TestEnterIsIgnoredAndEscapeDismisses(t *testing.T) {
	menu := opened(numbered(2), 2)

	if outcome := menu.Apply(enter); outcome != dropdown.Ignored {
		t.Errorf("enter gave %v", outcome)
	}
	if outcome := menu.Apply(escape); outcome != dropdown.Dismissed {
		t.Errorf("escape gave %v", outcome)
	}
	if outcome := menu.Apply(key.Key{Code: key.Rune, Value: 'a'}); outcome != dropdown.Ignored {
		t.Errorf("typing gave %v", outcome)
	}
	if outcome := menu.Apply(key.Key{Code: key.Enter, Mod: key.Alt}); outcome != dropdown.Ignored {
		t.Errorf("alt+enter gave %v", outcome)
	}
}

func TestEnterWithoutOptionsIsLeftToTheInput(t *testing.T) {
	menu := opened(nil, 0)

	if outcome := menu.Apply(enter); outcome != dropdown.Ignored {
		t.Errorf("enter gave %v", outcome)
	}
	if outcome := menu.Apply(tab); outcome != dropdown.Swallowed {
		t.Errorf("tab gave %v", outcome)
	}
	if got := plainRows(menu, 40); !reflect.DeepEqual(got, []string{"  nothing here"}) {
		t.Errorf("drew %q", got)
	}
}

func TestOverflowingOptionsNameWhatIsHidden(t *testing.T) {
	menu := opened(numbered(10), 30)

	rows := plainRows(menu, 40)
	if len(rows) != dropdown.MaxRows {
		t.Fatalf("drew %d rows, want %d", len(rows), dropdown.MaxRows)
	}
	if rows[0] != "› option 1" || rows[len(rows)-1] != "  ⋮ 23 more" {
		t.Errorf("drew %q", rows)
	}

	for range 8 {
		menu.Apply(down)
	}
	rows = plainRows(menu, 40)
	if rows[len(rows)-2] != "› option 9" || rows[len(rows)-1] != "  ⋮ 2 above, 21 below" {
		t.Errorf("drew %q after scrolling", rows)
	}
}

func TestTheHeightNeverShrinksWhileOpen(t *testing.T) {
	menu := opened(numbered(5), 5)
	menu.SetOptions(numbered(1), 1)

	rows := plainRows(menu, 40)
	if len(rows) != 5 || rows[0] != "› option 1" || rows[4] != "" {
		t.Errorf("drew %q", rows)
	}

	menu.Close()
	menu.Open()
	menu.SetOptions(numbered(1), 1)
	if rows := menu.Rows(40, dropdown.MaxRows); len(rows) != 1 {
		t.Errorf("drew %d rows after reopening", len(rows))
	}
}

func TestStartElisionKeepsTheTailOfAnOption(t *testing.T) {
	menu := opened([]dropdown.Option{{Label: "internal/app/harness/app.go"}}, 1)
	menu.SetElision(dropdown.ElideStart)

	if got := plainRows(menu, 14); got[0] != "› …ness/app.go" {
		t.Errorf("drew %q", got[0])
	}

	menu.SetElision(dropdown.ElideEnd)
	if got := plainRows(menu, 14); got[0] != "› internal/ap…" {
		t.Errorf("drew %q", got[0])
	}
}

func TestABudgetShortensTheDropdownAndKeepsTheSelectionInView(t *testing.T) {
	menu := opened(numbered(10), 10)

	rows := menu.Rows(40, 4)
	plain := make([]string, len(rows))
	for i, row := range rows {
		plain[i] = style.Plain(row)
	}
	want := []string{"› option 1", "  option 2", "  option 3", "  ⋮ 7 more"}
	if !reflect.DeepEqual(plain, want) {
		t.Errorf("drew %q, want %q", plain, want)
	}

	for range 5 {
		menu.Apply(down)
	}
	rows = menu.Rows(40, 4)
	for i, row := range rows {
		plain[i] = style.Plain(row)
	}
	want = []string{"  option 4", "  option 5", "› option 6", "  ⋮ 3 above, 4 below"}
	if !reflect.DeepEqual(plain, want) {
		t.Errorf("drew %q after moving, want %q", plain, want)
	}
}

func TestAOneRowBudgetDrawsOnlyTheSelection(t *testing.T) {
	menu := opened(numbered(10), 10)
	menu.Apply(down)

	rows := menu.Rows(40, 0)
	if len(rows) != 1 || style.Plain(rows[0]) != "› option 2" {
		t.Errorf("drew %q", rows)
	}
}

func TestAListLongerThanTheDropdownStopsAtItsEndsRatherThanWrapping(t *testing.T) {
	menu := opened(numbered(12), 12)

	menu.Apply(up)
	if got, _ := menu.Selected(); got != 0 {
		t.Errorf("up at the top selected %d", got)
	}

	for range 15 {
		menu.Apply(down)
	}
	if got, _ := menu.Selected(); got != 11 {
		t.Errorf("moving past the end selected %d", got)
	}
}

func TestAnIncompleteListStopsAtItsEndsRatherThanWrapping(t *testing.T) {
	menu := opened(numbered(10), 30)

	menu.Apply(up)
	if got, _ := menu.Selected(); got != 0 {
		t.Errorf("up at the top selected %d", got)
	}
	if menu.IsShortOfOptions() {
		t.Error("the top of the list was short of options")
	}

	for range 12 {
		menu.Apply(down)
	}
	if got, _ := menu.Selected(); got != 9 {
		t.Errorf("moving past the last held option selected %d", got)
	}
	if !menu.IsShortOfOptions() {
		t.Error("the last held option of an incomplete list was not short of options")
	}
}

func TestACompleteListIsNeverShortOfOptions(t *testing.T) {
	menu := opened(numbered(3), 3)
	menu.Apply(up)

	if got, _ := menu.Selected(); got != 2 || menu.IsShortOfOptions() {
		t.Errorf("selected %d, short of options %v", got, menu.IsShortOfOptions())
	}
}

func TestExtendingOptionsKeepsTheSelectionAndWindow(t *testing.T) {
	menu := opened(numbered(10), 30)
	for range 9 {
		menu.Apply(down)
	}
	before := plainRows(menu, 40)

	menu.ExtendOptions(numbered(20), 30)
	if got, _ := menu.Selected(); got != 9 {
		t.Errorf("extending moved the selection to %d", got)
	}
	after := plainRows(menu, 40)
	if !reflect.DeepEqual(before[:len(before)-1], after[:len(after)-1]) {
		t.Errorf("extending moved the window from %q to %q", before, after)
	}

	menu.Apply(down)
	if got, _ := menu.Selected(); got != 10 {
		t.Errorf("moving on after extending selected %d", got)
	}
}

func TestDetailsAlignAfterTheWidestLabel(t *testing.T) {
	menu := opened([]dropdown.Option{
		{Label: "/conf", Detail: "Edit the config."},
		{Label: "/expose", Detail: "Expose a port."},
		{Label: "/quit"},
	}, 3)

	want := []string{"› /conf    Edit the config.", "  /expose  Expose a port.", "  /quit"}
	if got := plainRows(menu, 40); !reflect.DeepEqual(got, want) {
		t.Errorf("drew %q", got)
	}
}

func TestANarrowRowCutsTheDetailBeforeTheLabel(t *testing.T) {
	menu := opened([]dropdown.Option{{Label: "/expose", Detail: "Expose a port."}}, 1)

	if got := plainRows(menu, 14); got[0] != "› /expose  Ex…" {
		t.Errorf("drew %q", got[0])
	}
}

func TestAStartElidedLabelKeepsItsTailBesideADetail(t *testing.T) {
	menu := opened([]dropdown.Option{{Label: "internal/app/harness/app.go", Detail: "The app."}}, 1)
	menu.SetElision(dropdown.ElideStart)

	if got := plainRows(menu, 14); got[0] != "› …ness/app.go" {
		t.Errorf("drew %q", got[0])
	}
}

func TestADetailWithNoRoomLeftIsDropped(t *testing.T) {
	menu := opened([]dropdown.Option{{Label: "/expose", Detail: "Expose a port."}}, 1)

	for columns, want := range map[int]string{
		9:  "› /expose",
		10: "› /expose",
		11: "› /expose",
		12: "› /expose  …",
		13: "› /expose  E…",
	} {
		if got := plainRows(menu, columns); got[0] != want {
			t.Errorf("drew %q at %d columns, want %q", got[0], columns, want)
		}
	}
}
