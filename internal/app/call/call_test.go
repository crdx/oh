package call

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/markdown"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/tool"
)

func elided(t *testing.T, label Label, room int) Label {
	t.Helper()

	elidedLabel, isLabel := label.Elide(room).(Label)
	if !isLabel {
		t.Fatalf("expected a label, got %T", elidedLabel)
	}

	return elidedLabel
}

func measured(metrics *tool.ToolCallMetrics) string {
	text := Measurements(metrics)
	if text == "" {
		return "✓"
	}

	return "✓ " + text
}

func TestMetricsAreShownAfterCalls(t *testing.T) {
	for name, test := range map[string]struct {
		metrics tool.ToolCallMetrics
		want    []string
	}{
		"output": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricOutput, Lines: 4, Bytes: 1200, TotalBytes: 1200},
			want:    []string{"4L ~400t"},
		},
		"empty output": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricOutput},
			want:    []string{"no output"},
		},
		"capped output": {
			metrics: tool.ToolCallMetrics{
				Kind:        tool.MetricOutput,
				Lines:       4,
				Bytes:       1200,
				TotalBytes:  1200,
				IsTruncated: true,
			},
			want: []string{"4L+ ~400t"},
		},
		"resources": {
			metrics: tool.ToolCallMetrics{
				Kind:       tool.MetricResources,
				CPUTime:    800 * time.Millisecond,
				PeakMemory: 92 << 20,
				Lines:      7,
				Bytes:      1200,
			},
			want: []string{"0.8s 7L ~400t 92M"},
		},
		"resources spent on nothing": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricResources},
			want:    []string{"no output"},
		},
		"resources without a cost worth naming": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricResources, Lines: 3, Bytes: 40},
			want:    []string{"3L"},
		},
		"read": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricRead, Lines: 42, Bytes: 1200},
			want:    []string{"42L ~400t"},
		},
		"list": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricList, Lines: 42},
			want:    []string{"42L"},
		},
		"image": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricImage, Bytes: 80_943, EstimatedTokens: 1536},
			want:    []string{"~1.5Kt"},
		},
		"small estimate hidden": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricWrite, Lines: 3, Bytes: 280},
			want:    []string{"3L"},
		},
		"estimate above threshold shown": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricWrite, Lines: 3, Bytes: 281},
			want:    []string{"3L ~100t"},
		},
		"diff": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricDiff, AddedLines: 3, RemovedLines: 2},
			want:    []string{"+3 −2"},
		},
		"search": {
			metrics: tool.ToolCallMetrics{Kind: tool.MetricSearch, Lines: 17, Bytes: 1200},
			want:    []string{"17L ~400t"},
		},
		"small capped output with a large total": {
			metrics: tool.ToolCallMetrics{
				Kind:        tool.MetricSearch,
				Lines:       2,
				Bytes:       280,
				TotalBytes:  1200,
				IsTruncated: true,
			},
			want: []string{"2L+ ~100t (of ~400t)"},
		},
		"capped search": {
			metrics: tool.ToolCallMetrics{
				Kind:        tool.MetricSearch,
				Lines:       100,
				Bytes:       32_000,
				TotalBytes:  80_000,
				IsTruncated: true,
			},
			want: []string{"100L+ ~11Kt (of ~29Kt)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := style.Plain(measured(&test.metrics))
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestMetricsUseTheirExpectedStyles(t *testing.T) {
	output := measured(&tool.ToolCallMetrics{Kind: tool.MetricOutput, Lines: 4, Bytes: 951})
	if want := style.Subtle("4L ~300t"); !strings.Contains(output, want) {
		t.Errorf("output metrics got %q, want styled %q", output, want)
	}

	emptyOutput := measured(&tool.ToolCallMetrics{Kind: tool.MetricOutput})
	if want := style.Subtle("no output"); !strings.Contains(emptyOutput, want) {
		t.Errorf("empty output metrics got %q, want styled %q", emptyOutput, want)
	}

	read := measured(&tool.ToolCallMetrics{Kind: tool.MetricRead, Lines: 45, Bytes: 951})
	if want := style.Subtle("45L ~300t"); !strings.Contains(read, want) {
		t.Errorf("read metrics got %q, want styled %q", read, want)
	}

	write := measured(&tool.ToolCallMetrics{Kind: tool.MetricWrite, Lines: 12, Bytes: 1200})
	if want := style.Subtle("12L ~400t"); !strings.Contains(write, want) {
		t.Errorf("write metrics got %q, want styled %q", write, want)
	}

	search := measured(&tool.ToolCallMetrics{
		Kind:        tool.MetricSearch,
		Lines:       23,
		Bytes:       1200,
		TotalBytes:  2400,
		IsTruncated: true,
	})
	if want := style.Subtle("23L+ ~400t (of ~900t)"); !strings.Contains(search, want) {
		t.Errorf("search metrics got %q, want styled %q", search, want)
	}

	exec := measured(&tool.ToolCallMetrics{
		Kind:       tool.MetricResources,
		PeakMemory: 26 << 20,
	})
	wantExec := style.Subtle("no output 26M")
	if !strings.Contains(exec, wantExec) {
		t.Errorf("exec metrics got %q, want styled %q", exec, wantExec)
	}

	edit := measured(&tool.ToolCallMetrics{Kind: tool.MetricDiff, AddedLines: 2, RemovedLines: 1})
	wantEdit := style.Success("+2") + style.Subtle(" ") + style.Failure("−1")
	if !strings.Contains(edit, wantEdit) {
		t.Errorf("edit metrics got %q, want styled %q", edit, wantEdit)
	}
}

func TestOnlyTheFocusedPartOfArgumentsIsPainted(t *testing.T) {
	label := Label{
		Name:     "read",
		Subject:  "cmd/oh/draw.go",
		ReadOnly: true,
		Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"},
	}
	want := style.Call("read") + " " + style.Subtle("cmd/oh/") + style.Subject("draw.go")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAccentAndTheFocusedPartOfArgumentsArePainted(t *testing.T) {
	label := Label{
		Name:        "skill",
		NameStyle:   style.Skill,
		Subject:     "/skills/guard-basics/SKILL.md",
		Emphasis:    tool.Emphasis{Kind: tool.EmphasisFocus, Value: "SKILL.md"},
		Accent:      "guard-basics",
		AccentStyle: style.Skill,
	}
	want := style.Skill("skill") + " " +
		style.Subtle("/skills/") + style.Skill("guard-basics") +
		style.Subtle("/") + style.Subject("SKILL.md")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestArgumentsWithSyntaxAreHighlighted(t *testing.T) {
	label := Label{
		Name:     "bash",
		Subject:  "echo one && true",
		Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
	}
	want := style.Change("bash") + " " + markdown.Emphasise(label.Subject, "bash")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAContinuedBashCommandUsesTheOrdinaryBashLabel(t *testing.T) {
	bashLabel := Label{
		Name:      "$",
		NameStyle: style.Shell,
		Subject:   "cd /work && just check",
		Emphasis:  tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
	}
	label := Label{Name: "job", Subject: "check", Continuation: []Label{bashLabel}}

	want := style.Change("job") + " " + style.Subject("check") + " " + bashLabel.Render()
	if got := label.Render(); got != want {
		t.Errorf("got %q, want the exact bash label after the job name", got)
	}
}

func TestSyntaxCanBeHighlightedFromSourceBeyondTheDisplayedSubject(t *testing.T) {
	label := Label{
		Name:    "bash",
		Subject: "cat <<'EOF'",
		Emphasis: tool.Emphasis{
			Kind:   tool.EmphasisSyntax,
			Value:  "bash",
			Source: "cat <<'EOF'\nhello\nEOF",
		},
	}
	want := style.Change("bash") + " " + style.Function("cat") + style.Block(" ") +
		style.Operator("<<") + style.Block("'EOF'")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestElidedBashKeepsItsEmphasisFromTheCompleteCommand(t *testing.T) {
	source := "go list ordinary"
	for name, test := range map[string]struct {
		argumentRoom int
		plain        string
		want         string
	}{
		"inside executable": {
			2,
			"g…",
			style.Function("g") + style.Function(width.Ellipsis),
		},
		"at parameter start": {
			4,
			"go …",
			style.Function("go") + style.Block(" ") + style.Function(width.Ellipsis),
		},
		"inside parameter": {
			6,
			"go li…",
			style.Function("go") + style.Block(" ") + style.Function("li") + style.Function(width.Ellipsis),
		},
		"at parameter end": {
			8,
			"go list…",
			style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(width.Ellipsis),
		},
		"inside later argument": {
			12,
			"go list ord…",
			style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(" ord") + style.Block(width.Ellipsis),
		},
	} {
		label := Label{
			Name:     "bash",
			Subject:  source,
			Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
		}
		got := elided(t, label, len("bash ")+test.argumentRoom).renderSubject()

		if got != test.want {
			t.Errorf("%s: got %q, want %q", name, got, test.want)
		}
		if plain := style.Plain(got); plain != test.plain {
			t.Errorf("%s: got plain text %q, want %q", name, plain, test.plain)
		}
		if cells := style.Width(got); cells > test.argumentRoom {
			t.Errorf("%s: used %d cells, want at most %d", name, cells, test.argumentRoom)
		}
	}
}

func TestElidedBashCountsWideUnicodeInTerminalCells(t *testing.T) {
	argumentRoom := 8
	label := Label{
		Name:     "bash",
		Subject:  "echo 日本語 later",
		Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
	}
	got := elided(t, label, len("bash ")+argumentRoom).renderSubject()
	want := style.Function("echo") + style.Block(" ") + style.Function("日") + style.Function(width.Ellipsis)

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if plain := style.Plain(got); plain != "echo 日…" {
		t.Errorf("got plain text %q, want %q", plain, "echo 日…")
	}
	if cells := style.Width(got); cells > argumentRoom {
		t.Errorf("used %d cells, want at most %d", cells, argumentRoom)
	}
}

func TestAPathInTheDetailCanBeFocused(t *testing.T) {
	label := Label{
		Name:      "grep",
		Subject:   "text",
		Qualifier: "in cmd/oh/draw.go",
		Emphasis:  tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"},
	}
	want := style.Change("grep") + " " + style.Subject("text") + " " +
		style.Qualifier("in cmd/oh/") + style.Subject("draw.go")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestALabelIsColouredByWhetherItsCallWrites(t *testing.T) {
	label := Label{Name: "write", Subject: "main.go"}

	if got := label.Render(); got != style.Change("write")+" "+style.Subject("main.go") {
		t.Errorf("expected a call that writes to be painted as one, got %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for want, took := range map[string]time.Duration{
		"0.0s":   0,
		"0.9s":   999 * time.Millisecond,
		"1s":     1200 * time.Millisecond,
		"43s":    43*time.Second + 800*time.Millisecond,
		"59s":    59*time.Second + 999*time.Millisecond,
		"1m00s":  time.Minute,
		"12m34s": 12*time.Minute + 34*time.Second,
		"1h40m":  100 * time.Minute,
		"4d04h":  100 * time.Hour,
	} {
		if got := util.FormatDuration(took); got != want {
			t.Errorf("expected %q, got %q", want, got)
		}

		if len(want) > durationWidth {
			t.Errorf("expected %q to fit the column of %d", want, durationWidth)
		}
	}
}
