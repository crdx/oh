package painter

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/output"
)

func reasoningHeadingCases() []struct {
	name    string
	thought string
	want    string
} {
	return []struct {
		name    string
		thought string
		want    string
	}{
		{"heading and body", "**Checking the file**\n\nI\u2019m checking the file.", "I\u2019m checking the file."},
		{"body starts with emphasis", "**Checking**\n\n*the body*", "*the body*"},
		{"body starts with bold", "**Checking**\n\n**the body**", "**the body**"},
		{"body starts with a repeated heading", "**Checking**\n\n**Checking**\n\nThe body.", "**Checking**\n\nThe body."},
		{"body starts with a list", "**Checking**\n\n- first\n- second", "- first\n- second"},
		{"body starts with a fence", "**Checking**\n\n```go\nfunc main() {}\n```", "```go\nfunc main() {}\n```"},
		{"title alone", "**Checking the file**", "**Checking the file**"},
		{"title with blank line", "**Checking the file**\n\n", "**Checking the file**\n\n"},
		{"whitespace body", "**Checking the file**\n\n \t\n", "**Checking the file**\n\n \t\n"},
		{"spaced body", "**Checking the file**\n\n \tI\u2019m checking.\n\nThen reading.", " \tI\u2019m checking.\n\nThen reading."},
		{"only the leading heading", "**Checking**\n\nI\u2019m checking.\n\n**Reading**\n\nThen reading.", "I\u2019m checking.\n\n**Reading**\n\nThen reading."},
		{"single newline", "**Checking**\nI\u2019m checking.", "**Checking**\nI\u2019m checking."},
		{"extra newline", "**Checking**\n\n\nI\u2019m checking.", "\nI\u2019m checking."},
		{"carriage returns", "**Checking**\r\n\r\nI\u2019m checking.", "**Checking**\r\n\r\nI\u2019m checking."},
		{"space after title", "**Checking** \n\nI\u2019m checking.", "**Checking** \n\nI\u2019m checking."},
		{"leading space", " **Checking**\n\nI\u2019m checking.", " **Checking**\n\nI\u2019m checking."},
		{"leading prose", "First **Checking**\n\nI\u2019m checking.", "First **Checking**\n\nI\u2019m checking."},
		{"hash heading", "# Checking\n\nI\u2019m checking.", "# Checking\n\nI\u2019m checking."},
		{"underscore heading", "__Checking__\n\nI\u2019m checking.", "__Checking__\n\nI\u2019m checking."},
		{"bold and italic", "***Checking***\n\nI\u2019m checking.", "***Checking***\n\nI\u2019m checking."},
		{"two bold spans", "**Checking** and **reading**\n\nI\u2019m checking.", "**Checking** and **reading**\n\nI\u2019m checking."},
		{"empty title", "****\n\nI\u2019m checking.", "****\n\nI\u2019m checking."},
		{"blank title", "** **\n\nI\u2019m checking.", "** **\n\nI\u2019m checking."},
		{"multiline title", "**Checking\nthe file**\n\nI\u2019m checking.", "**Checking\nthe file**\n\nI\u2019m checking."},
		{"unclosed title", "**Checking\n\nI\u2019m checking.", "**Checking\n\nI\u2019m checking."},
		{"fenced heading", "```\n**Checking**\n\nI\u2019m checking.\n```", "```\n**Checking**\n\nI\u2019m checking.\n```"},
		{"long title and body", "**" + strings.TrimSpace(strings.Repeat("Checking the file ", 40)) + "**\n\nI\u2019m checking.", "I\u2019m checking."},
		{"long title alone", "**" + strings.TrimSpace(strings.Repeat("Checking the file ", 40)) + "**", "**" + strings.TrimSpace(strings.Repeat("Checking the file ", 40)) + "**"},
	}
}

func TestOnlyAnExactLeadingReasoningHeadingWithABodyIsRemoved(t *testing.T) {
	for _, testCase := range reasoningHeadingCases() {
		t.Run(testCase.name, func(t *testing.T) {
			got, _ := withoutReasoningHeading(testCase.thought)
			if got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestAReasoningHeadingArrivingADeltaAtATimeDrawsWhatArrived(t *testing.T) {
	for _, testCase := range reasoningHeadingCases() {
		for name, rendering := range map[string]output.ReasoningRendering{
			"plain":    output.ReasoningPlain,
			"markdown": output.ReasoningMarkdown,
		} {
			t.Run(testCase.name+"/"+name, func(t *testing.T) {
				for _, columns := range []int{1, 12, 40} {
					var renderer markdown.IncrementalRenderer
					var plain plainThought
					var reflow paragraphReflow
					var styles rowMemory
					for at := 1; at <= len(testCase.thought); at++ {
						thought := testCase.thought[:at]
						rows := renderReasoningWith(
							&renderer,
							&plain,
							&reflow,
							&styles,
							thought,
							columns,
							rendering,
							false,
							false,
							link.Roots{},
						)
						var freshRenderer markdown.IncrementalRenderer
						var freshPlain plainThought
						var freshReflow paragraphReflow
						var freshStyles rowMemory
						want := renderReasoningWith(
							&freshRenderer,
							&freshPlain,
							&freshReflow,
							&freshStyles,
							thought,
							columns,
							rendering,
							false,
							false,
							link.Roots{},
						)
						if !slices.Equal(rows, want) {
							t.Fatalf("byte %d at %d columns drew %q, want %q", at, columns, rows, want)
						}
					}
					rows := renderReasoningWith(&renderer, &plain, &reflow, &styles, testCase.thought, columns, rendering, true, false, link.Roots{})
					if want := RenderReasoning(testCase.thought, columns, rendering); !slices.Equal(rows, want) {
						t.Fatalf("finished at %d columns drew %q, want %q", columns, rows, want)
					}
				}
			})
		}
	}
}
