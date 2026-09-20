package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/style"
)

const answer = `# Findings

The **first** thing is that ` + "`read`" + ` is *cheap*, and the second is not.

` + "```go" + `
func main() {
	fmt.Println("hello")
}
` + "```" + `

- one
- two
    - nested

| Name | What it does  |  Cost |
|:-----|:-------------:|------:|
| read | takes a file  |     1 |
| bash | runs a thing  |    40 |

> and that is that
`

const columns = 80

func TestAnAnswerDrawnADeltaAtATimeIsTheAnswerDrawnAtOnce(t *testing.T) {
	var last []string

	for length := range len(answer) + 1 {
		last = Render(answer[:length], columns)

		for _, row := range last {
			if got := style.Width(row); got > columns {
				t.Fatalf("a prefix of %d gave a row of %d cells: %q", length, got, row)
			}
		}
	}

	if want := Render(answer, columns); !slices.Equal(last, want) {
		t.Errorf("the last drawing was %q, want %q", last, want)
	}
}

func TestATableWidensAsItsRowsArrive(t *testing.T) {
	short := Render("| a | b |\n|---|---|\n| x | y |\n", columns)
	long := Render("| a | b |\n|---|---|\n| x | y |\n| a much wider cell | y |\n", columns)

	if style.Width(long[0]) <= style.Width(short[0]) {
		t.Errorf("expected the table to widen, got %q and %q", short[0], long[0])
	}

	for _, row := range long {
		if style.Width(row) != style.Width(long[0]) {
			t.Errorf("expected every row the same width, got %q against %q", row, long[0])
		}
	}
}

func TestATableTooNarrowForItsColumnsIsAbandoned(t *testing.T) {
	source := "| a | b |\n|---|---|\n| x | y |"

	got := style.Plain(strings.Join(Render(source, 8), " "))

	for _, want := range []string{"a", "b", "x", "y"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive, got %q", want, got)
		}
	}

	if strings.ContainsAny(got, "┌│└") {
		t.Errorf("expected no borders to be drawn, got %q", got)
	}
}

func TestAnUnclosedFenceIsStillCode(t *testing.T) {
	got := Render("```go\nfunc main() {\n", columns)

	if len(got) != 1 || !strings.Contains(style.Plain(got[0]), "func main() {") {
		t.Errorf("expected the open block to be drawn as code, got %q", got)
	}

	if strings.HasPrefix(style.Plain(got[0]), " ") {
		t.Errorf("expected the code to start at the left edge, got %q", got[0])
	}
}

func TestAnUnclosedDelimiterIsLiteral(t *testing.T) {
	for text, want := range map[string]string{
		"**bold":         "**bold",
		"**bold**":       "bold",
		"a `code` span":  "a code span",
		"`**not bold**`": "**not bold**",
		"~~struck~~":     "struck",
		"[text](url)":    "text (url)",
		"\\*starred\\*":  "*starred*",
	} {
		got := style.Plain(strings.Join(Render(text, columns), "\n"))

		if got != want {
			t.Errorf("Render(%q) drew %q, want %q", text, got, want)
		}
	}
}

func TestMarkdownLinksAndBareURLsCanBeTerminalHyperlinks(t *testing.T) {
	source := "[docs](https://example.test/docs) https://example.test/bare person@example.test"
	plainRows := Render(source, columns)
	if plain := strings.Join(plainRows, "\n"); strings.Contains(plain, "\x1b]8;;") {
		t.Errorf("plain rendering contains a terminal hyperlink: %q", plain)
	}

	linked := strings.Join(RenderWithHyperlinks(source, columns), "\n")
	if got := strings.Count(linked, "\x1b]8;;"); got != 6 {
		t.Errorf("got %d OSC 8 openings and closings, want 6: %q", got, linked)
	}
	for _, address := range []string{
		"https://example.test/docs",
		"https://example.test/bare",
		"mailto:person@example.test",
	} {
		if !strings.Contains(linked, "\x1b]8;;"+address+"\x1b\\") {
			t.Errorf("missing hyperlink to %q in %q", address, linked)
		}
	}
}

func TestUnsupportedMarkdownDestinationsStayPlain(t *testing.T) {
	got := strings.Join(RenderWithHyperlinks("[unsafe](javascript:alert(1))", columns), "\n")
	if strings.Contains(got, "\x1b]8;;") {
		t.Errorf("unsupported destination became a terminal hyperlink: %q", got)
	}
}

func TestAPathIsLinkedBeforeItWraps(t *testing.T) {
	linkRoot, relativePath := linkedPathFixture(t)
	source := "```bash\n    " + relativePath + "\n```"
	rows := RenderWithHyperlinksUnder(source, 12, link.Roots{Workspace: linkRoot})

	if got, want := style.Plain(strings.Join(rows, "")), "    "+relativePath; got != want {
		t.Errorf("wrapped path drew %q, want %q", got, want)
	}

	for i, row := range rows {
		if strings.Count(row, "\x1b]8;;file://") != 1 || strings.Count(row, "\x1b]8;;\x1b\\") != 1 {
			t.Errorf("row %d does not contain one complete path hyperlink: %q", i, row)
		}
	}
}

func TestAPathInATableIsLinkedBeforeItWraps(t *testing.T) {
	linkRoot, relativePath := linkedPathFixture(t)
	source := "| Path |\n|---|\n| " + relativePath + " |"
	rows := RenderWithHyperlinksUnder(source, 20, link.Roots{Workspace: linkRoot})
	linked := strings.Join(rows, "\n")
	openings := strings.Count(linked, "\x1b]8;;file://")
	closings := strings.Count(linked, "\x1b]8;;\x1b\\")

	if openings < 2 || closings != openings {
		t.Errorf("wrapped table path has %d openings and %d closings: %q", openings, closings, linked)
	}
}

func linkedPathFixture(t *testing.T) (string, string) {
	t.Helper()

	linkRoot := t.TempDir()
	relativePath := "somewhere/a-very-long-patch-file-name.patch"
	path := filepath.Join(linkRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	return linkRoot, relativePath
}

func TestANestedListKeepsItsChildrenWithTheirParent(t *testing.T) {
	source := "- `one`\n\n  - child\n  - another\n- `two`\n\n  - child"
	got := style.Plain(strings.Join(Render(source, columns), "\n"))
	want := "• one\n  • child\n  • another\n• two\n  • child"

	if got != want {
		t.Errorf("nested list drew %q, want %q", got, want)
	}
}

func TestTheBlocksAreDrawnWithoutTheirPunctuation(t *testing.T) {
	got := style.Plain(strings.Join(Render(answer, columns), "\n"))

	for _, gone := range []string{"#", "```", "**", "- one", "|"} {
		if strings.Contains(got, gone) {
			t.Errorf("expected %q to be drawn rather than shown, got %q", gone, got)
		}
	}

	for _, want := range []string{"Findings", "• one", "│ and that is that", "┌", "read"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to be drawn, got %q", want, got)
		}
	}
}

func TestAFenceIsClosedOnlyByALineThatIsAFence(t *testing.T) {
	got := style.Plain(strings.Join(Render("```\none\n```go still open\ntwo\n```\nafter", columns), "\n"))

	for _, want := range []string{"one", "```go still open", "two", "after"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive, got %q", want, got)
		}
	}
}

func TestAStyleInsideAnotherKeepsTheOneOutsideIt(t *testing.T) {
	got := Render("**one `two` three**", columns)[0]

	if strings.Count(got, "\x1b[1m") < 2 {
		t.Errorf("expected the bold to be opened again after the inner style, got %q", got)
	}
}

func TestATableHeadingIsDrawnAsMarkdownRatherThanShown(t *testing.T) {
	got := style.Plain(strings.Join(Render("| **Name** | `Kind` |\n|---|---|\n| a | b |", columns), "\n"))

	if strings.Contains(got, "**") || strings.Contains(got, "`") {
		t.Errorf("expected the heading punctuation to be drawn, got %q", got)
	}
}

func TestAMermaidFenceIsSafeWhileItStreams(t *testing.T) {
	source := "```mermaid\ngraph LR\nA[Start] --> B[Done]\n```"
	for length := range len(source) + 1 {
		for _, row := range Render(source[:length], columns) {
			if got := style.Width(row); got > columns {
				t.Fatalf("a prefix of %d gave a row of %d cells: %q", length, got, row)
			}
		}
	}
}

func TestAStreamingMermaidRendererRetainsOnlyDiagramsThatStillFit(t *testing.T) {
	stream := &StreamRenderer{}
	invalid := "```mermaid\ngraph LR\nA -->"
	if got := style.Plain(strings.Join(stream.Render(invalid, columns), "\n")); !strings.Contains(got, "graph LR") {
		t.Fatalf("expected source before the first valid rendering, got %q", got)
	}

	valid := "```mermaid\ngraph LR\nA --> B"
	validRendering := style.Plain(strings.Join(stream.Render(valid, columns), "\n"))
	if strings.Contains(validRendering, "graph LR") {
		t.Fatalf("expected a rendered diagram, got %q", validRendering)
	}

	cached := style.Plain(strings.Join(stream.Render(valid+"\nB -->", columns), "\n"))
	if cached != validRendering {
		t.Errorf("got cached rendering %q, want %q", cached, validRendering)
	}

	narrow := style.Plain(strings.Join(stream.Render(valid+"\nB -->", 5), "\n"))
	if !strings.Contains(narrow, "graph") {
		t.Errorf("expected source when the cached diagram no longer fits, got %q", narrow)
	}

	stream.Reset()
	reset := style.Plain(strings.Join(stream.Render(valid+"\nB -->", columns), "\n"))
	if !strings.Contains(reset, "graph LR") {
		t.Errorf("expected Reset to forget the cached diagram, got %q", reset)
	}
}

func TestAMermaidDiagramUsesTheNormalTerminalForeground(t *testing.T) {
	source := "```mermaid\ngraph LR\nA --> B\n```"
	got := strings.Join(Render(source, columns), "\n")
	if got != style.Plain(got) {
		t.Errorf("expected no colour styling around Mermaid output, got %q", got)
	}
}

func TestAMermaidFenceIsDrawnAsADiagram(t *testing.T) {
	source := "```mermaid\ngraph LR\nA[Start] --> B[Done]\n```"
	got := style.Plain(strings.Join(Render(source, columns), "\n"))

	for _, want := range []string{"Start", "Done", "┌", "►"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the diagram to contain %q, got %q", want, got)
		}
	}

	if strings.Contains(got, "graph LR") || strings.Contains(got, "-->") {
		t.Errorf("expected the Mermaid source to be drawn rather than shown, got %q", got)
	}
}

func TestMermaidDiagramsRenderOrFallBackAtNarrowWidths(t *testing.T) {
	for name, test := range map[string]struct {
		source             string
		wideMarker         string
		narrowSourceMarker string
		shouldRenderNarrow bool
	}{
		"branching flowchart": {
			source:             "graph TD\nrequest[Receive] --> validate[Validate]\nvalidate --> accept[Accept]\nvalidate --> reject[Reject]\nsubgraph audit [Audit trail]\naccept --> recorded[Record success]\nreject --> failed[Record failure]\nend",
			wideMarker:         "Record success",
			narrowSourceMarker: "graph TD",
		},
		"sequence with note and fragment": {
			source:             "sequenceDiagram\nparticipant U as User\nparticipant A as App\nU->>A: Open\nNote right of A: Ready\nloop Retry\nA-->>U: Update\nend",
			wideMarker:         "Ready",
			narrowSourceMarker: "sequenceDiagram",
			shouldRenderNarrow: true,
		},
		"entity relationship attributes": {
			source:             "erDiagram\nCUSTOMER {\nint id PK\nstring name\n}\nORDER {\nint id PK\nint customer_id FK\n}\nCUSTOMER ||--o{ ORDER : places",
			wideMarker:         "customer_id",
			narrowSourceMarker: "erDiagram",
		},
	} {
		markdown := "```mermaid\n" + test.source + "\n```"
		wide := style.Plain(strings.Join(Render(markdown, 100), "\n"))
		if !strings.Contains(wide, test.wideMarker) || strings.Contains(wide, test.narrowSourceMarker) {
			t.Errorf("%s: expected a rendered wide diagram, got %q", name, wide)
		}

		narrow := style.Plain(strings.Join(Render(markdown, 40), "\n"))
		isRendered := !strings.Contains(narrow, test.narrowSourceMarker)
		if isRendered != test.shouldRenderNarrow {
			t.Errorf("%s: narrow rendered=%v, want %v; got %q", name, isRendered, test.shouldRenderNarrow, narrow)
		}

		tiny := style.Plain(strings.Join(Render(markdown, 12), "\n"))
		if !strings.Contains(strings.ReplaceAll(tiny, "\n", ""), test.narrowSourceMarker) {
			t.Errorf("%s: expected source fallback at 12 columns, got %q", name, tiny)
		}
	}
}

func TestAMermaidFenceFallsBackToSourceWhenItCannotBeDrawn(t *testing.T) {
	for name, test := range map[string]struct {
		source  string
		want    string
		columns int
	}{
		"malformed":          {"```mermaid\ngraph SIDEWAYS\nA --> B\n```", "A --> B", columns},
		"unsupported syntax": {"```mermaid\ngraph LR\nA -.-> B\n```", "A -.-> B", columns},
		"resource limit":     {"```mermaid\ngraph LR\nA[" + strings.Repeat("x", 300) + "]\n```", "graph LR", columns},
		"too wide":           {"```mermaid\ngraph LR\nAlpha --> Bravo --> Charlie\n```", "Alpha -->", 12},
	} {
		got := style.Plain(strings.Join(Render(test.source, test.columns), "\n"))
		if !strings.Contains(got, test.want) {
			t.Errorf("%s: expected %q to survive, got %q", name, test.want, got)
		}
	}
}

func TestADiagramTooWideToDrawSaysWhatItNeeded(t *testing.T) {
	source := "```mermaid\ngraph LR\nAlpha --> Bravo --> Charlie\n```"

	got := style.Plain(strings.Join(Render(source, 30), "\n"))

	if !strings.Contains(got, "Diagram needs 39 columns.") {
		t.Errorf("got %q, want it to say the columns the diagram needed", got)
	}
	if !strings.Contains(got, "Alpha -->") {
		t.Errorf("got %q, want the source beneath what it said", got)
	}
}

func TestADiagramOutgrowingTheTerminalIsDrawnTheSameStreamedAsWhole(t *testing.T) {
	source := "```mermaid\ngraph LR\nAlpha --> Bravo --> Charlie\n```"
	const narrow = 20

	var stream StreamRenderer
	var streamed []string
	for length := range len(source) + 1 {
		streamed = stream.Render(source[:length], narrow)
	}

	whole := Render(source, narrow)

	if !slices.Equal(streamed, whole) {
		t.Errorf("streamed %q, want the %q a replay draws", streamed, whole)
	}
}

func FuzzMermaidFenceStreaming(fuzzer *testing.F) {
	for _, source := range []string{
		"graph LR\nA --> B",
		"sequenceDiagram\nA->>B: hello",
		"graph LR\nA -.-> B",
		"\xff\xfe",
	} {
		fuzzer.Add(source, uint8(columns))
	}

	fuzzer.Fuzz(func(t *testing.T, source string, fuzzedColumns uint8) {
		if len(source) > 128 {
			return
		}
		markdown := "```mermaid\n" + source + "\n```"
		availableColumns := int(fuzzedColumns) + 2
		for length := range len(markdown) + 1 {
			for _, row := range Render(markdown[:length], availableColumns) {
				if got := style.Width(row); got > availableColumns {
					t.Fatalf("a prefix of %d gave a row of %d cells: %q", length, got, row)
				}
			}
		}
	})
}

func TestAFileIsHighlightedFromItsNameAcrossLines(t *testing.T) {
	source := "package main\n\nfunc main() {}\n"
	highlighted := HighlightFile("main.go", source)
	if style.Plain(highlighted) != source {
		t.Errorf("highlighting changed the text to %q", style.Plain(highlighted))
	}
	if highlighted == source || !strings.Contains(highlighted, style.Keyword("package")) {
		t.Errorf("expected Go highlighting, got %q", highlighted)
	}
}

func TestCodeIsHighlightedWhereTheLanguageIsKnown(t *testing.T) {
	known := Render("```go\nfunc main() {}\n```", columns)
	unknown := Render("```nosuchlanguage\nfunc main() {}\n```", columns)

	if style.Plain(known[0]) != style.Plain(unknown[0]) {
		t.Errorf("expected the same code either way, got %q and %q", known[0], unknown[0])
	}

	if known[0] == unknown[0] {
		t.Errorf("expected the known language to be highlighted, got %q", known[0])
	}

	if strings.Count(unknown[0], "\x1b[") != 2 {
		t.Errorf("expected the unknown language to be drawn as one run, got %q", unknown[0])
	}
}

func TestADiffIsPaintedByWhatEachLineDoes(t *testing.T) {
	for line, want := range map[string]style.Style{
		"@@ -1,2 +1,2 @@": style.Hunk,
		"-gone":           style.DeletedText,
		"+here":           style.InsertedText,
		" kept":           style.Block,
	} {
		if got := Emphasise(line, "diff"); got != want(line) {
			t.Errorf("%q: got %q, want %q", line, got, want(line))
		}
	}
}

func TestBashCommandNamesAndFirstParametersAreHighlighted(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   string
	}{
		"simple": {
			"go test ./cmd/oh",
			style.Function("go") + style.Block(" ") + style.Function("test") + style.Block(" ./cmd/oh"),
		},
		"assignment": {
			"GOCACHE=/tmp/io-go-cache go list",
			style.Block("GOCACHE") + style.Operator("=") + style.Block("/tmp/io-go-cache") +
				style.Block(" ") + style.Function("go") + style.Block(" ") + style.Function("list"),
		},
		"pipeline": {
			"printf one | grep one",
			style.Function("printf") + style.Block(" ") + style.Function("one") + style.Block(" ") +
				style.Operator("|") + style.Block(" ") + style.Function("grep") + style.Block(" ") +
				style.Function("one"),
		},
		"combined output pipeline": {
			"printf one |& grep one",
			style.Function("printf") + style.Block(" ") + style.Function("one") + style.Block(" ") +
				style.Operator("|&") + style.Block(" ") + style.Function("grep") + style.Block(" ") +
				style.Function("one"),
		},
		"background": {
			"sleep 1 & wait",
			style.Function("sleep") + style.Block(" ") + style.Function("1") + style.Block(" ") +
				style.Operator("&") + style.Block(" ") + style.Function("wait"),
		},
		"and conditional": {
			"go test && git status --short",
			style.Function("go") + style.Block(" ") + style.Function("test") + style.Block(" ") +
				style.Operator("&&") + style.Block(" ") + style.Function("git") + style.Block(" ") +
				style.Function("status") + style.Block(" --short"),
		},
		"or conditional": {
			"go test || git status --short",
			style.Function("go") + style.Block(" ") + style.Function("test") + style.Block(" ") +
				style.Operator("||") + style.Block(" ") + style.Function("git") + style.Block(" ") +
				style.Function("status") + style.Block(" --short"),
		},
		"command substitution": {
			"echo one $(go list)",
			style.Function("echo") + style.Block(" ") + style.Function("one") + style.Block(" $(") +
				style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(")"),
		},
		"executable only": {
			"true",
			style.Function("true"),
		},
	} {
		if got := Emphasise(test.source, "bash"); got != test.want {
			t.Errorf("%s: got %q, want %q", name, got, test.want)
		}
	}
}

func TestBashHereDocumentOpeningLinesAreHighlightedFromTheCompleteCommand(t *testing.T) {
	prefix := "cd /tmp/io && python3 - <<'PY'"
	source := prefix + "\nprint('hello')\nPY"
	want := style.Function("cd") + style.Block(" ") + style.Function("/tmp/io") + style.Block(" ") +
		style.Operator("&&") + style.Block(" ") + style.Function("python3") + style.Block(" ") +
		style.Function("-") + style.Block(" ") + style.Operator("<<") + style.Block("'PY'")

	if got := Highlight(source, prefix, "bash", false); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMalformedBashFallsBackToOnePlainRun(t *testing.T) {
	source := "if true; then"
	if got, want := Emphasise(source, "bash"), style.Block(source); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBashAssignmentsAndRedirectionsAreHighlighted(t *testing.T) {
	source := `PATCH=/tmp/change.patch; : >"$PATCH"`
	want := style.Block("PATCH") + style.Operator("=") + style.Block("/tmp/change.patch") +
		style.Operator(";") + style.Block(" ") + style.Function(":") + style.Block(" ") + style.Operator(">") +
		style.Block(`"$PATCH"`)

	if got := Emphasise(source, "bash"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	prefix := `PATCH=/tmp/cha`
	wantPrefix := style.Block("PATCH") + style.Operator("=") + style.Block("/tmp/cha") + style.Block("…")
	if got := Highlight(source, prefix, "bash", true); got != wantPrefix {
		t.Errorf("elided: got %q, want %q", got, wantPrefix)
	}
}

func TestNestedBashSpansDoNotSliceBackwards(t *testing.T) {
	source := `RESULT=$(printf one)`
	want := style.Block("RESULT") + style.Operator("=") + style.Block("$(printf one)")

	if got := Emphasise(source, "bash"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBashForLoopKeywordsAreHighlighted(t *testing.T) {
	source := "for path in one; do true; done"
	want := style.Keyword("for") + style.Block(" path ") + style.Keyword("in") +
		style.Block(" one; ") + style.Keyword("do") +
		style.Block(" ") + style.Function("true") + style.Operator(";") + style.Block(" ") +
		style.Keyword("done")

	if got := Emphasise(source, "bash"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBashCompoundKeywordsAreHighlighted(t *testing.T) {
	for name, test := range map[string]struct {
		source   string
		keywords []string
	}{
		"while": {"while true; do echo one; done", []string{"while", "do", "done"}},
		"until": {"until false; do echo one; done", []string{"until", "do", "done"}},
		"if":    {"if true; then echo one; fi", []string{"if", "then", "fi"}},
		"else":  {"if true; then echo one; else echo two; fi", []string{"if", "then", "else", "fi"}},
		"elif":  {"if true; then echo one; elif false; then echo two; fi", []string{"if", "then", "elif", "fi"}},
		"case":  {"case one in one) echo one;; esac", []string{"case", "in", "esac"}},
	} {
		got := Emphasise(test.source, "bash")

		for _, keyword := range test.keywords {
			if !strings.Contains(got, style.Keyword(keyword)) {
				t.Errorf("%s: expected %q painted as a keyword, got %q", name, keyword, got)
			}
		}
	}
}

func TestACaseItemTerminatorIsAnOperator(t *testing.T) {
	got := Emphasise("case one in one) echo one;; esac", "bash")

	if !strings.Contains(got, style.Operator(";;")) {
		t.Errorf("expected the terminator painted as an operator, got %q", got)
	}
}

func TestRegexpSyntaxIsHighlighted(t *testing.T) {
	source := `^(foo|bar)+\s[0-9]{2,4}$`
	want := style.Keyword("^") + style.Block("(") + style.Block("foo") +
		style.Operator("|") + style.Block("bar") + style.Block(")") +
		style.Operator("+") + style.Keyword(`\s`) + style.Block("[") +
		style.Block("0") + style.Operator("-") + style.Block("9") +
		style.Block("]") + style.Operator("{2,4}") + style.Keyword("$")

	if got := Emphasise(source, "regexp"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestARegexpCharacterClassHoldsNoAnchors(t *testing.T) {
	source := `[^0-9]`
	want := style.Block("[") + style.Block("^0") +
		style.Operator("-") + style.Block("9") + style.Block("]")

	if got := Emphasise(source, "regexp"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestElidedRegexpKeepsTheStyleOfAPartialEscape(t *testing.T) {
	source := `foo\p{Greek}bar`
	prefix := `foo\p{G`
	want := style.Block("foo") + style.Keyword(`\p{G`) + style.Keyword("…")

	if got := Highlight(source, prefix, "regexp", true); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoBlockOverrunsANarrowTerminal(t *testing.T) {
	blocks := []string{
		"```\nabc\n```", "> quoted", "# heading", "- item", "| a | b |\n|---|---|\n| x | y |",
		"a paragraph of several words", "---",
	}

	for _, block := range blocks {
		for cells := 1; cells <= 12; cells++ {
			for _, row := range Render(block, cells) {
				if got := style.Width(row); got > cells {
					t.Errorf("Render(%q, %d) gave a row of %d cells: %q", block, cells, got, row)
				}
			}
		}
	}
}

func TestAStyleOverAnotherLeavesTextAloneWhereNothingIsPainted(t *testing.T) {
	paintsNothing := func(format any, args ...any) string { return fmt.Sprint(format) }

	if got := over(paintsNothing, "hello"); got != "hello" {
		t.Errorf("got %q, want the text left alone when there is no reset to find", got)
	}
}

func TestMarkdownKnowsWhetherItEndsWithATable(t *testing.T) {
	for name, test := range map[string]struct {
		markdown      string
		isTableEnding bool
	}{
		"nothing at all":     {markdown: ""},
		"plain prose":        {markdown: "a paragraph with | a pipe in it"},
		"a header alone":     {markdown: "| tool | cost |\n"},
		"a delimited header": {markdown: "| tool | cost |\n|---|---|\n", isTableEnding: true},
		"a table with rows":  {markdown: "| tool | cost |\n|---|---|\n| read | 1 |\n", isTableEnding: true},
		"prose after one":    {markdown: "| tool | cost |\n|---|---|\n\nand then some prose\n"},
		"a fenced pipe":      {markdown: "```\n| tool | cost |\n|---|---|\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := EndsWithTable(test.markdown); got != test.isTableEnding {
				t.Errorf("EndsWithTable() = %t, want %t", got, test.isTableEnding)
			}
		})
	}
}

func TestAStreamRendererKnowsWhetherItsTailIsMermaid(t *testing.T) {
	for name, test := range map[string]struct {
		markdown      string
		isTailMermaid bool
	}{
		"plain prose":     {markdown: "plain prose"},
		"ordinary code":   {markdown: "```go\nfunc main() {}"},
		"backtick fence":  {markdown: "```mermaid\ngraph LR\nA --> B", isTailMermaid: true},
		"completed fence": {markdown: "```mermaid\ngraph LR\nA --> B\n```", isTailMermaid: true},
		"tilde fence":     {markdown: "~~~mermaid\ngraph LR\nA --> B", isTailMermaid: true},
		"quoted fence":    {markdown: "> ```mermaid\n> graph LR\n> A --> B", isTailMermaid: true},
		"listed fence":    {markdown: "- ```mermaid\n  graph LR\n  A --> B", isTailMermaid: true},
		"following prose": {markdown: "```mermaid\ngraph LR\nA --> B\n```\n\nafter"},
		"invalid diagram": {markdown: "```mermaid\ngraph LR\nA -->"},
	} {
		t.Run(name, func(t *testing.T) {
			var renderer StreamRenderer
			renderer.Render(test.markdown, 100)

			if got := renderer.IsTailMermaid(); got != test.isTailMermaid {
				t.Errorf("IsTailMermaid() = %t, want %t", got, test.isTailMermaid)
			}
		})
	}
}

func TestTheLastValidMermaidRemainsTheTailWhileItsSourceIsInvalid(t *testing.T) {
	var renderer StreamRenderer
	renderer.Render("```mermaid\ngraph LR\nA --> B", 100)
	renderer.Render("```mermaid\ngraph LR\nA --> B\nB -->", 100)

	if !renderer.IsTailMermaid() {
		t.Error("expected the cached Mermaid rendering to remain the tail")
	}
}

func TestAnImageWrittenOutNamesItsAddressWithoutAGap(t *testing.T) {
	for name, test := range map[string]struct {
		markdown string
		want     string
	}{
		"no words at all":  {markdown: "![](/pictures/chart.png)", want: "(/pictures/chart.png)"},
		"words of its own": {markdown: "![a chart](/pictures/chart.png)", want: "a chart (/pictures/chart.png)"},
	} {
		t.Run(name, func(t *testing.T) {
			rows := Render(test.markdown, 60)

			if got := style.Plain(strings.Join(rows, "\n")); got != test.want {
				t.Errorf("wrote %q, want %q", got, test.want)
			}
		})
	}
}
