package painter

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/util/strutil"
)

const maximumFuzzedThoughtBytes = 1024

func drawPlainThought(plain *plainThought, reflow *paragraphReflow, thought string, columns int, isSettled bool) []string {
	var renderer markdown.IncrementalRenderer
	var styles rowMemory

	return renderReasoningWith(
		&renderer,
		plain,
		reflow,
		&styles,
		thought,
		columns,
		output.ReasoningPlain,
		isSettled,
		false,
		link.Roots{},
	)
}

func requirePlainThoughtKeepsItsSettledRows(t *testing.T, thought string, columns int) {
	t.Helper()

	var plain plainThought
	var reflow paragraphReflow
	var settled []string

	requireKept := func(rows []string, description string) {
		t.Helper()

		if len(rows) < len(settled) || !slices.Equal(rows[:len(settled)], settled) {
			t.Fatalf("%s changed a settled row at %d columns\nsettled: %q\ndrawn:   %q", description, columns, settled, rows)
		}
	}

	for at := 1; at <= len(thought); at++ {
		if at < len(thought) && !utf8.RuneStart(thought[at]) {
			continue
		}

		rows := drawPlainThought(&plain, &reflow, thought[:at], columns, false)
		requireKept(rows, fmt.Sprintf("byte %d", at))
		if stable := min(reflow.StableRows(), len(rows)); stable > len(settled) {
			settled = slices.Clone(rows[:stable])
		}
	}

	requireKept(drawPlainThought(&plain, &reflow, thought, columns, true), "the finished thought")
}

func FuzzAPlainThoughtNeverChangesARowItSettled(fuzzer *testing.F) {
	for _, thought := range []string{
		"Checking the flag `--yolo first, since it changes what the sandbox allows and every later step depends on it.",
		"See <https://example.com/a/long/address/that/wraps/over/rows> for the details of it all.",
		"Use **bold that runs on and on across several rows before it closes** and then more.",
		"An unclosed *star that never closes and keeps going for a good while longer than a row.",
		"| what | when |\n| --- | --- |\n| read | first |\n\nafter",
		"Fish &amp; chips &copy and more words to wrap the row along.",
		"- one\n- two\n\n> quoted\n\n# heading\n\n---\n\ntext",
		"[a link](https://example.com/with/a/long/path) in the middle of a line that wraps.",
		"snake_case_name and _private and __init__ and trailing_ words to wrap the row.",
		"```go\nfunc main() {}\n```\nafter the fence",
		strings.Repeat("`", 40) + "\n\n" + strings.Repeat("`", 40) + "[]",
	} {
		for _, columns := range []uint8{1, 3, 12, 40} {
			fuzzer.Add(thought, columns)
		}
	}

	fuzzer.Fuzz(func(t *testing.T, thought string, columnsChoice uint8) {
		isDrawable := strutil.ControlFreeLength(thought, 0) == len(thought)
		if len(thought) > maximumFuzzedThoughtBytes || !utf8.ValidString(thought) || !isDrawable {
			t.Skip()
		}

		requirePlainThoughtKeepsItsSettledRows(t, thought, 1+int(columnsChoice)%maximumFuzzedColumns)
	})
}

const maximumFuzzedColumns = 60

func BenchmarkALongPlainThoughtLineArriving(benchmark *testing.B) {
	line := strings.Repeat("Checking `the flag` and *some* words and <https://example.com> too ", 60)

	benchmark.ReportAllocs()
	for benchmark.Loop() {
		var plain plainThought
		var reflow paragraphReflow
		for at := 4; at <= len(line); at += 4 {
			drawPlainThought(&plain, &reflow, line[:at], 100, false)
		}
	}
}
