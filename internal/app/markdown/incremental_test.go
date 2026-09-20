package markdown

import (
	"slices"
	"strings"
	"testing"
)

func TestIncrementalRendererMatchesMarkdownThatCanChangeEarlierBlocks(t *testing.T) {
	for name, source := range map[string]string{
		"paragraphs":              "first paragraph\n\nsecond paragraph\n\nthird paragraph",
		"provisional heading":     "0\n#000",
		"tabs before blocks":      "\tindented code\n\nparagraph\n\n\tmore code",
		"multi-byte text":         "héllo 🐞\n\nsecond paragraph\n\nthird",
		"fenced code with blanks": "before\n\n```go\nfunc one() {}\n\nfunc two() {}\n```\n\nafter",
		"table":                   "before\n\n| one | two |\n| --- | --- |\n| a | b |\n\nafter",
		"loose list":              "before\n\n- one\n\n- two\n\nafter",
		"late list continuation":  "before\n\n- one\n\n- two\n\nafter\n\n- unrelated",
		"late link reference":     "see [the note][note]\n\nordinary text\n\n[note]: https://example.com",
		"early link reference":    "[note]: https://example.com\n\nordinary text\n\nsee [the note][note]",
		"quoted blocks":           "> first\n>\n> second\n\nafter",
		"html blocks":             "<div>\nfirst\n\nsecond\n</div>\n\nafter",
		"mermaid blocks":          "```mermaid\ngraph LR\nA --> B\n```\n\nafter\n\n```mermaid\ngraph LR\nB --> C",
	} {
		for _, columns := range []int{0, 1, 10, 40, 100} {
			t.Run(name, func(t *testing.T) {
				var incremental IncrementalRenderer
				var baseline StreamRenderer
				for at := 1; at <= len(source); at++ {
					want := baseline.Render(source[:at], columns)
					got := incremental.Render(source[:at], columns)
					if !slices.Equal(got, want) {
						t.Fatalf("at %d columns, byte %d produced different rows\nwant: %q\ngot:  %q", columns, at, want, got)
					}
					if incremental.IsTailMermaid() != baseline.IsTailMermaid() {
						t.Fatalf("at %d columns, byte %d disagreed about a Mermaid tail", columns, at)
					}
				}
			})
		}
	}
}

func TestIncrementalHyperlinkRenderingMatchesTheCompleteStream(t *testing.T) {
	source := "first paragraph\n\n[linked words](https://example.test/path) and https://example.test/bare"
	var incremental IncrementalRenderer
	var baseline StreamRenderer

	for at := 1; at <= len(source); at++ {
		want := baseline.render(source[:at], Options{Columns: 24, ShouldRenderHyperlinks: true})
		got := incremental.RenderWithHyperlinks(source[:at], 24)
		if !slices.Equal(got, want) {
			t.Fatalf("byte %d produced different rows\nwant: %q\ngot:  %q", at, want, got)
		}
	}
}

func TestIncrementalRendererDisablesForDocumentWideState(t *testing.T) {
	for name, source := range map[string]string{
		"link reference":           "see [the note][note]\n\n[note]: https://example.com",
		"backtick Mermaid":         "```mermaid\ngraph LR\nA --> B",
		"tilde Mermaid in a quote": "> ~~~mermaid\n> graph LR\n> A --> B",
	} {
		t.Run(name, func(t *testing.T) {
			var renderer IncrementalRenderer
			renderer.Render(source, 100)
			if !renderer.isDisabled {
				t.Error("expected incremental rendering to be disabled")
			}
		})
	}
}

func TestIncrementalRendererResetsForAWidthOrSourceChange(t *testing.T) {
	const first = "first paragraph\n\nsecond paragraph\n\nthird paragraph"
	const second = "a replacement stream\n\nwith another paragraph"

	var renderer IncrementalRenderer
	for at := 1; at <= len(first); at++ {
		renderer.Render(first[:at], 40)
	}

	t.Run("disabled renderer", func(t *testing.T) {
		var disabled IncrementalRenderer
		disabled.Render("```mermaid\ngraph LR\nA --> B", 40)
		got := disabled.Render(second, 40)
		var baseline StreamRenderer
		want := baseline.Render(second, 40)
		if !slices.Equal(got, want) || disabled.isDisabled {
			t.Errorf("want reset rows %q, got %q with disabled=%t", want, got, disabled.isDisabled)
		}
	})

	for name, test := range map[string]struct {
		source  string
		columns int
	}{
		"width":  {source: first, columns: 20},
		"source": {source: second, columns: 40},
	} {
		t.Run(name, func(t *testing.T) {
			got := renderer.Render(test.source, test.columns)
			var baseline StreamRenderer
			want := baseline.Render(test.source, test.columns)
			if !slices.Equal(got, want) {
				t.Errorf("want %q, got %q", want, got)
			}
		})
	}
}

func FuzzIncrementalRenderer(fuzzer *testing.F) {
	for _, source := range []string{
		"one paragraph\n\na second\n\nand a third",
		"- one\n\n- two\n\nafter",
		"```go\none\n\ntwo\n```\n\nafter",
		"see [note][n]\n\n[n]: https://example.com",
		"```mermaid\ngraph LR\nA --> B",
		"\tindented code\n\nparagraph\n\n\tmore code",
		"héllo 🐞\n\nsecond\n\nthird",
	} {
		fuzzer.Add(source, uint8(40))
	}

	fuzzer.Fuzz(func(t *testing.T, source string, columnsByte uint8) {
		const maximumSourceBytes = 512
		if len(source) > maximumSourceBytes {
			t.Skip()
		}

		columns := int(columnsByte)
		var incremental IncrementalRenderer
		var baseline StreamRenderer
		for at := 1; at <= len(source); at++ {
			want := baseline.Render(source[:at], columns)
			got := incremental.Render(source[:at], columns)
			if !slices.Equal(got, want) {
				t.Fatalf("byte %d produced different rows\nwant: %q\ngot:  %q", at, want, got)
			}
			if incremental.IsTailMermaid() != baseline.IsTailMermaid() {
				t.Fatalf("byte %d disagreed about a Mermaid tail", at)
			}
		}
	})
}

func BenchmarkSyntheticFullRender(benchmark *testing.B) {
	for _, test := range syntheticBenchmarks() {
		benchmark.Run(test.name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				var source strings.Builder
				var renderer StreamRenderer
				for _, delta := range test.deltas {
					source.WriteString(delta)
					renderer.Render(source.String(), 100)
				}
			}
		})
	}
}

func BenchmarkSyntheticIncremental(benchmark *testing.B) {
	for _, test := range syntheticBenchmarks() {
		benchmark.Run(test.name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				var source strings.Builder
				var renderer IncrementalRenderer
				for _, delta := range test.deltas {
					source.WriteString(delta)
					renderer.Render(source.String(), 100)
				}
			}
		})
	}
}

func syntheticBenchmarks() []struct {
	name   string
	deltas []string
} {
	paragraph := strings.Repeat("ordinary words for a streamed paragraph. ", 5)
	return []struct {
		name   string
		deltas []string
	}{
		{name: "2kb paragraphs", deltas: splitDeltas(strings.Repeat(paragraph+"\n\n", 10), 5)},
		{name: "14kb paragraphs", deltas: splitDeltas(strings.Repeat(paragraph+"\n\n", 70), 5)},
		{name: "14kb single block", deltas: splitDeltas(strings.Repeat(paragraph, 70), 5)},
	}
}

func splitDeltas(source string, size int) []string {
	var deltas []string
	for len(source) > 0 {
		at := min(size, len(source))
		deltas = append(deltas, source[:at])
		source = source[at:]
	}
	return deltas
}
