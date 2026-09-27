package trigger_test

import (
	"fmt"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/trigger"
)

type fakeSource struct {
	symbol         rune
	items          []trigger.Result
	textSuffix     string
	opens          int
	announceChange func()
	queries        []string
	limits         []int
}

func (self *fakeSource) Symbol() rune {
	return self.symbol
}

func (self *fakeSource) Elision() dropdown.Elision {
	return dropdown.ElideEnd
}

func (self *fakeSource) Find(runes []rune, cursor int) (trigger.Word, bool) {
	return trigger.FindWord(runes, cursor, self.symbol)
}

func (self *fakeSource) Open(announceChange func()) {
	self.opens++
	self.announceChange = announceChange
}

func (self *fakeSource) Results(word trigger.Word, limit int) trigger.Results {
	self.queries = append(self.queries, word.Query)
	self.limits = append(self.limits, limit)

	var items []trigger.Result
	total := 0
	for _, item := range self.items {
		if !strings.HasPrefix(item.Text, word.Query) {
			continue
		}
		total++
		if len(items) < limit {
			items = append(items, trigger.Result{
				Label:       item.Text,
				Text:        trigger.WordText(self.symbol, item.Text, word.IsQuoted, item.IsOpenEnded) + self.textSuffix,
				IsOpenEnded: item.IsOpenEnded,
			})
		}
	}

	return trigger.Results{Items: items, Total: total, Placeholder: "nothing for " + string(self.symbol)}
}

type fakeEditor struct {
	runes       []rune
	cursor      int
	isSearching bool
}

func (self *fakeEditor) Runes() []rune {
	return self.runes
}

func (self *fakeEditor) Cursor() int {
	return self.cursor
}

func (self *fakeEditor) Replace(start int, end int, text string) {
	self.runes = append(append(append([]rune(nil), self.runes[:start]...), []rune(text)...), self.runes[end:]...)
	self.cursor = start + len([]rune(text))
}

func (self *fakeEditor) IsSearching() bool {
	return self.isSearching
}

func (self *fakeEditor) typeText(text string) {
	self.Replace(self.cursor, self.cursor, text)
}

func typeInto(completer *trigger.Completer, editor *fakeEditor, text string) {
	for _, value := range text {
		editor.typeText(string(value))
		completer.Typed(editor, key.Key{Code: key.Rune, Value: value})
		completer.Sync(editor)
	}
}

func files() *fakeSource {
	return &fakeSource{symbol: '@', items: []trigger.Result{
		{Text: "cmd/", IsOpenEnded: true},
		{Text: "cmd/main.go"},
		{Text: "go.mod"},
	}}
}

func tags() *fakeSource {
	return &fakeSource{symbol: '#', items: []trigger.Result{{Text: "bug"}, {Text: "build"}}}
}

func plainRows(completer *trigger.Completer) []string {
	rows := completer.Rows(40, dropdown.MaxRows)
	for i, row := range rows {
		rows[i] = style.Plain(row)
	}
	return rows
}

func TestFindReadsTheWordUnderTheCursor(t *testing.T) {
	for name, test := range map[string]struct {
		text      string
		cursor    int
		want      trigger.Word
		wantFound bool
	}{
		"bare symbol":                {text: "@", cursor: 1, want: trigger.Word{Start: 0, End: 1}, wantFound: true},
		"after a word":               {text: "look at @int", cursor: 12, want: trigger.Word{Start: 8, End: 12, Query: "int"}, wantFound: true},
		"cursor inside the word":     {text: "@internal x", cursor: 4, want: trigger.Word{Start: 0, End: 9, Query: "int"}, wantFound: true},
		"after a newline":            {text: "one\n@two", cursor: 8, want: trigger.Word{Start: 4, End: 8, Query: "two"}, wantFound: true},
		"an opening quote":           {text: `@"`, cursor: 2, want: trigger.Word{Start: 0, End: 2, IsQuoted: true}, wantFound: true},
		"a space inside quotes":      {text: `see @"my do`, cursor: 11, want: trigger.Word{Start: 4, End: 11, Query: "my do", IsQuoted: true}, wantFound: true},
		"inside closed quotes":       {text: `@"my docs" x`, cursor: 5, want: trigger.Word{Start: 0, End: 10, Query: "my ", IsQuoted: true}, wantFound: true},
		"an unclosed quote runs on":  {text: `@"my docs next`, cursor: 7, want: trigger.Word{Start: 0, End: 9, Query: "my do", IsQuoted: true}, wantFound: true},
		"after closed quotes":        {text: `@"my docs" `, cursor: 10},
		"a quote within a word":      {text: `x@"my do`, cursor: 8},
		"a quote on an earlier line": {text: "@\"one\ntwo", cursor: 8},
		"within a word":              {text: "mail foo@bar", cursor: 12},
		"after a space":              {text: "@foo ", cursor: 5},
		"before the symbol":          {text: "@foo", cursor: 0},
		"another symbol":             {text: "#foo", cursor: 4},
		"no symbol":                  {text: "plain", cursor: 5},
	} {
		t.Run(name, func(t *testing.T) {
			got, found := trigger.FindWord([]rune(test.text), test.cursor, '@')
			if found != test.wantFound {
				t.Fatalf("found %v, want %v", found, test.wantFound)
			}
			if got != test.want {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestReplacementLeavesAnOpenEndedResultOpenAndClosesTheRest(t *testing.T) {
	word := trigger.Word{Start: 0, End: 4, Query: "int"}
	runes := []rune("@int")

	if got := trigger.Replacement(word, trigger.Result{Text: "@internal/", IsOpenEnded: true}, runes); got != "@internal/" {
		t.Errorf("open-ended replacement is %q", got)
	}
	if got := trigger.Replacement(word, trigger.Result{Text: "@main.go"}, runes); got != "@main.go " {
		t.Errorf("closed replacement is %q", got)
	}

	beforeSpace := trigger.Word{Start: 0, End: 3, Query: "ma"}
	if got := trigger.Replacement(beforeSpace, trigger.Result{Text: "#main"}, []rune("#ma rest")); got != "#main" {
		t.Errorf("closed replacement before a space is %q", got)
	}
}

func TestWordTextQuotesOnlyWhatNeedsIt(t *testing.T) {
	for name, test := range map[string]struct {
		text        string
		isQuoted    bool
		isOpenEnded bool
		want        string
	}{
		"plain":                     {text: "main.go", want: "@main.go"},
		"plain open-ended":          {text: "cmd/", isOpenEnded: true, want: "@cmd/"},
		"a space":                   {text: "my docs/a b.txt", want: `@"my docs/a b.txt"`},
		"a space open-ended":        {text: "my docs/", isOpenEnded: true, want: `@"my docs/`},
		"already quoted":            {text: "main.go", isQuoted: true, want: `@"main.go"`},
		"already quoted open-ended": {text: "cmd/", isQuoted: true, isOpenEnded: true, want: `@"cmd/`},
	} {
		t.Run(name, func(t *testing.T) {
			if got := trigger.WordText('@', test.text, test.isQuoted, test.isOpenEnded); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestEachSymbolOpensItsOwnSource(t *testing.T) {
	fileSource, tagSource := files(), tags()
	completer := trigger.New(fileSource, tagSource)
	editor := &fakeEditor{}

	typeInto(completer, editor, "#b")
	if got := plainRows(completer); len(got) != 2 || got[0] != "› bug" {
		t.Errorf("drew %q for a tag", got)
	}

	typeInto(completer, editor, " @go")
	if got := plainRows(completer); got[0] != "› go.mod" || got[1] != "" {
		t.Errorf("drew %q for a file", got)
	}
	if fileSource.opens != 1 || tagSource.opens != 1 {
		t.Errorf("opened files %d times and tags %d times, want once each", fileSource.opens, tagSource.opens)
	}
}

func TestAnUnchangedQueryIsNotAskedAgain(t *testing.T) {
	source := files()
	completer := trigger.New(source)
	editor := &fakeEditor{}

	typeInto(completer, editor, "@c")
	completer.Sync(editor)
	typeInto(completer, editor, "m")

	if got := strings.Join(source.queries, ","); got != ",c,cm" {
		t.Errorf("asked for %q", got)
	}
}

func TestAChangeAnnouncedBySourceIsMatchedAgain(t *testing.T) {
	source := files()
	completer := trigger.New(source)
	editor := &fakeEditor{}

	typeInto(completer, editor, "@z")
	if got := plainRows(completer); got[0] != "  nothing for @" {
		t.Fatalf("drew %q", got)
	}

	source.items = append(source.items, trigger.Result{Text: "zebra"})
	source.announceChange()
	select {
	case <-completer.Changes():
	default:
		t.Fatal("the change was not announced")
	}
	completer.Receive()

	if got := plainRows(completer); got[0] != "› zebra" {
		t.Errorf("drew %q after the change", got)
	}
}

func TestChoosingAnOpenEndedResultKeepsCompleting(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@c")
	if !completer.Apply(editor, key.Key{Code: key.Enter}) {
		t.Fatal("enter was not taken")
	}

	if got := string(editor.runes); got != "@cmd/" {
		t.Errorf("completed to %q", got)
	}
	if selected, _ := completer.Selected(); selected.Label != "cmd/" || !completer.IsOpen() {
		t.Errorf("selected %+v with the dropdown open %v", selected, completer.IsOpen())
	}
}

func TestChoosingAClosedResultEndsTheWord(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@go")
	completer.Apply(editor, key.Key{Code: key.Enter})

	if got := string(editor.runes); got != "@go.mod " {
		t.Errorf("completed to %q", got)
	}
	if completer.IsOpen() {
		t.Error("the dropdown stayed open")
	}
}

func TestTypingOutTheOnlyResultClosesTheDropdown(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@go.mo")
	if !completer.IsOpen() {
		t.Fatal("the dropdown closed before the word was whole")
	}

	typeInto(completer, editor, "d")
	if completer.IsOpen() {
		t.Error("the dropdown stayed open over a word it had nothing to add to")
	}
}

func TestEnterLeavesAWordTypedOutWholeToBeSent(t *testing.T) {
	completer := trigger.New(&fakeSource{symbol: '#', items: []trigger.Result{{Text: "bug"}, {Text: "build"}}, textSuffix: " "})
	editor := &fakeEditor{}

	typeInto(completer, editor, "#bug")
	if !completer.IsOpen() {
		t.Fatal("the dropdown closed over a word that goes on to arguments")
	}

	if completer.Apply(editor, key.Key{Code: key.Enter}) {
		t.Error("enter was taken to choose what was already written")
	}
	if got := string(editor.runes); got != "#bug" {
		t.Errorf("the word became %q", got)
	}
	if completer.IsOpen() {
		t.Error("the dropdown stayed open over a word being sent")
	}
}

func TestTabStillCarriesAWordTypedOutWholeOnToItsArguments(t *testing.T) {
	completer := trigger.New(&fakeSource{symbol: '#', items: []trigger.Result{{Text: "bug", IsOpenEnded: true}}, textSuffix: " "})
	editor := &fakeEditor{}

	typeInto(completer, editor, "#bug")
	if !completer.Apply(editor, key.Key{Code: key.Rune, Value: '\t'}) {
		t.Fatal("tab was not taken")
	}
	if got := string(editor.runes); got != "#bug " {
		t.Errorf("the word became %q", got)
	}
}

func TestEnterStillChoosesAmongSeveralResults(t *testing.T) {
	completer := trigger.New(&fakeSource{symbol: '#', items: []trigger.Result{{Text: "bug"}, {Text: "bugs"}}, textSuffix: " "})
	editor := &fakeEditor{}

	typeInto(completer, editor, "#bug")
	if !completer.Apply(editor, key.Key{Code: key.Enter}) {
		t.Error("enter was left alone while a longer result still extends the word")
	}
}

func TestTypingOutOneOfSeveralResultsKeepsTheDropdownOpen(t *testing.T) {
	completer := trigger.New(&fakeSource{symbol: '#', items: []trigger.Result{{Text: "bug"}, {Text: "bugs"}}})
	editor := &fakeEditor{}

	typeInto(completer, editor, "#bug")
	if !completer.IsOpen() {
		t.Error("the dropdown closed over a word a longer result still extends")
	}
}

func TestOnlyTypingTheSymbolOrOpeningOpensTheDropdown(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	editor.typeText("look at @go")
	completer.Sync(editor)
	if completer.IsOpen() {
		t.Fatal("a word that arrived whole opened the dropdown")
	}

	typeInto(completer, editor, ".")
	if completer.IsOpen() {
		t.Error("typing into a closed word opened the dropdown")
	}

	if !completer.Open(editor) || !completer.IsOpen() {
		t.Fatal("opening over a word left the dropdown closed")
	}
	if got := plainRows(completer); len(got) != 1 || got[0] != "› go.mod" {
		t.Errorf("drew %q", got)
	}
}

func TestOpeningAwayFromAWordDoesNothing(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}
	editor.typeText("plain @go ")

	if completer.Open(editor) || completer.IsOpen() {
		t.Error("opened away from a word")
	}
}

func TestTypingTheSymbolWithinAWordOpensNothing(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "foo@")
	if completer.IsOpen() {
		t.Error("a symbol within a word opened the dropdown")
	}
}

func TestTypingTheSymbolBeforeAWordOpensOverIt(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{runes: []rune("go")}

	typeInto(completer, editor, "@")
	if !completer.IsOpen() {
		t.Fatal("a symbol typed before a word stayed closed")
	}
	if got := plainRows(completer); got[0] != "› cmd/" {
		t.Errorf("drew %q for the empty query before the cursor", got)
	}
}

func TestOnlyAPlainOrShiftedSymbolOpens(t *testing.T) {
	for name, test := range map[string]struct {
		modifier   key.Modifier
		wantIsOpen bool
	}{
		"plain":   {wantIsOpen: true},
		"shifted": {modifier: key.Shift, wantIsOpen: true},
		"alt":     {modifier: key.Alt},
		"ctrl":    {modifier: key.Ctrl},
	} {
		t.Run(name, func(t *testing.T) {
			completer := trigger.New(files())
			editor := &fakeEditor{}
			editor.typeText("@")

			completer.Typed(editor, key.Key{Code: key.Rune, Value: '@', Mod: test.modifier})
			if completer.IsOpen() != test.wantIsOpen {
				t.Errorf("open %v, want %v", completer.IsOpen(), test.wantIsOpen)
			}
		})
	}
}

func TestMovingOntoAnotherWordClosesTheDropdown(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@c @")
	editor.cursor -= 2
	completer.Sync(editor)

	if completer.IsOpen() {
		t.Error("the dropdown followed the cursor onto another word")
	}
}

func TestEscapeDismissesUntilTheWordEnds(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@")
	if !completer.Apply(editor, key.Key{Code: key.Escape}) || completer.IsOpen() {
		t.Fatal("escape did not dismiss the dropdown")
	}

	typeInto(completer, editor, "c")
	if completer.IsOpen() {
		t.Error("typing reopened a dismissed word")
	}

	typeInto(completer, editor, " @")
	if !completer.IsOpen() {
		t.Error("a new word stayed closed")
	}
}

func TestSearchingOpensNothing(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{isSearching: true}
	editor.typeText("@")

	if completer.Open(editor) || completer.Typed(editor, key.Key{Code: key.Rune, Value: '@'}) {
		t.Error("opened during a search")
	}
}

func TestSearchingClosesTheDropdown(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@")
	editor.isSearching = true
	completer.Sync(editor)

	if completer.IsOpen() {
		t.Error("the dropdown stayed open during a search")
	}
}

func TestKeysTheDropdownDoesNotOwnAreLeftAlone(t *testing.T) {
	completer := trigger.New(files())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@")

	if completer.Apply(editor, key.Key{Code: key.Rune, Value: 'x'}) {
		t.Error("typing was taken by the dropdown")
	}
	if completer.Apply(editor, key.Key{Code: key.Enter, Mod: key.Alt}) {
		t.Error("alt+enter was taken by the dropdown")
	}
}

func manyFiles(count int) *fakeSource {
	source := &fakeSource{symbol: '@'}
	for i := range count {
		source.items = append(source.items, trigger.Result{Text: fmt.Sprintf("file%03d", i)})
	}
	return source
}

func TestMovingOntoTheLastHeldResultFetchesMore(t *testing.T) {
	source := manyFiles(450)
	completer := trigger.New(source)
	editor := &fakeEditor{}
	typeInto(completer, editor, "@")

	down := key.Key{Code: key.Down}
	for range 199 {
		completer.Apply(editor, down)
	}
	if got := fmt.Sprint(source.limits); got != "[200 400]" {
		t.Errorf("asked for %s", got)
	}
	if selected, _ := completer.Selected(); selected.Label != "file199" {
		t.Errorf("selected %q after fetching more", selected.Label)
	}

	completer.Apply(editor, down)
	if selected, _ := completer.Selected(); selected.Label != "file200" {
		t.Errorf("selected %q past the first page", selected.Label)
	}

	rows := plainRows(completer)
	if got := rows[len(rows)-1]; got != "  ⋮ 194 above, 249 below" {
		t.Errorf("the note reads %q", got)
	}
}

func TestUpAtTheTopOfAnIncompleteListStaysPut(t *testing.T) {
	completer := trigger.New(manyFiles(450))
	editor := &fakeEditor{}
	typeInto(completer, editor, "@")

	completer.Apply(editor, key.Key{Code: key.Up})
	if selected, _ := completer.Selected(); selected.Label != "file000" {
		t.Errorf("up at the top selected %q", selected.Label)
	}
	if rows := plainRows(completer); rows[len(rows)-1] != "  ⋮ 443 more" {
		t.Errorf("the note reads %q", rows[len(rows)-1])
	}
}

func TestANewQueryStartsFromTheFirstPage(t *testing.T) {
	source := manyFiles(450)
	completer := trigger.New(source)
	editor := &fakeEditor{}
	typeInto(completer, editor, "@")
	for range 199 {
		completer.Apply(editor, key.Key{Code: key.Down})
	}

	typeInto(completer, editor, "f")
	if got := source.limits[len(source.limits)-1]; got != 200 {
		t.Errorf("a new query asked for %d", got)
	}
}

func spacedFiles() *fakeSource {
	return &fakeSource{symbol: '@', items: []trigger.Result{
		{Text: "a b.txt"},
		{Text: "my docs/", IsOpenEnded: true},
		{Text: "my docs/q r.txt"},
	}}
}

func TestChoosingAPathWithASpaceQuotesIt(t *testing.T) {
	completer := trigger.New(spacedFiles())
	editor := &fakeEditor{}

	typeInto(completer, editor, "see @a")
	completer.Apply(editor, key.Key{Code: key.Enter})

	if got := string(editor.runes); got != `see @"a b.txt" ` {
		t.Errorf("completed to %q", got)
	}
	if completer.IsOpen() {
		t.Error("the dropdown stayed open after a quoted file")
	}
}

func TestAQuotedDirectoryKeepsCompletingInsideItsQuotes(t *testing.T) {
	completer := trigger.New(spacedFiles())
	editor := &fakeEditor{}

	typeInto(completer, editor, "@my")
	completer.Apply(editor, key.Key{Code: key.Enter})
	if got := string(editor.runes); got != `@"my docs/` {
		t.Fatalf("completed the directory to %q", got)
	}
	if !completer.IsOpen() {
		t.Fatal("the dropdown closed inside a quoted directory")
	}
	if selected, _ := completer.Selected(); selected.Label != "my docs/" {
		t.Errorf("selected %q inside the quoted directory", selected.Label)
	}

	completer.Apply(editor, key.Key{Code: key.Down})
	completer.Apply(editor, key.Key{Code: key.Enter})
	if got := string(editor.runes); got != `@"my docs/q r.txt" ` {
		t.Errorf("completed the file to %q", got)
	}
}

func TestOpeningAQuoteAsksAgain(t *testing.T) {
	source := files()
	completer := trigger.New(source)
	editor := &fakeEditor{}

	typeInto(completer, editor, `@"`)
	if got := strings.Join(source.queries, ","); got != "," {
		t.Errorf("asked for %q", got)
	}

	completer.Apply(editor, key.Key{Code: key.Down})
	completer.Apply(editor, key.Key{Code: key.Enter})
	if got := string(editor.runes); got != `@"cmd/main.go" ` {
		t.Errorf("completed to %q", got)
	}
}
