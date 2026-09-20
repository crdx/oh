package call_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/call"
	"crdx.org/io/internal/app/link"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/job"
)

func label() call.Label {
	return call.Label{Name: "grep", Subject: "hello", Qualifier: "in internal"}
}

func check(t *testing.T, room int, name string, subject string, qualifier string) {
	t.Helper()

	elidedLabel, _ := label().Elide(room).(call.Label)

	if elidedLabel.Name != name || elidedLabel.Subject != subject || elidedLabel.Qualifier != qualifier {
		t.Errorf(
			"in %d columns expected %q %q %q, got %q %q %q",
			room, name, subject, qualifier, elidedLabel.Name, elidedLabel.Subject, elidedLabel.Qualifier,
		)
	}
}

func TestALabelThatFitsIsLeftAlone(t *testing.T) {
	check(t, 22, "grep", "hello", "in internal")
	check(t, 80, "grep", "hello", "in internal")
}

func TestAResultLinkWrapsOnlyTheCallName(t *testing.T) {
	resultURI := "oh://tool-result?call=one&session=tame-impala"
	rendered := call.Label{Name: "read", Subject: "main.go", ResultURI: resultURI}.Render()
	if !strings.Contains(rendered, "\x1b]8;;"+resultURI+"\x1b\\") {
		t.Errorf("result URI is missing from %q", rendered)
	}
	if strings.Index(rendered, "\x1b]8;;\x1b\\") > strings.Index(rendered, "main.go") {
		t.Errorf("subject is inside the result link in %q", rendered)
	}
}

func TestAReadLinkIncludesItsRangeAndOpensAtTheFirstLine(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		lineRange string
		firstLine string
	}{
		{lineRange: "10-14", firstLine: "10"},
		{lineRange: "10+", firstLine: "10"},
		{lineRange: "1-5", firstLine: "1"},
	} {
		label := call.LabelFor(agent.Event{
			Name: "read",
			FallbackRendering: agent.FallbackRendering{
				Subject: "main.go",
				Note:    test.lineRange,
			},
		}, nil, nil)
		label.PathRoots = link.Roots{Workspace: workspace}
		rendered := label.Render()

		wantAddress := "file://" + filepath.ToSlash(path) + "#" + test.firstLine
		if !strings.Contains(rendered, "\x1b]8;;"+wantAddress+"\x1b\\") {
			t.Errorf("%s: read URI does not start at its first line in %q", test.lineRange, rendered)
		}
		if closeAt := strings.Index(rendered, "\x1b]8;;\x1b\\"); closeAt < strings.Index(rendered, test.lineRange) {
			t.Errorf("%s: line range is outside the hyperlink in %q", test.lineRange, rendered)
		}
	}
}

func TestAReadOfModelScratchShowsAndLinksTheHostScratchAlias(t *testing.T) {
	scratch := t.TempDir()
	path := filepath.Join(scratch, "io", "cmd", "oh", "output", "region.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	roots := link.Roots{Scratch: scratch}
	label := call.LabelFor(agent.Event{
		Name: "read",
		FallbackRendering: agent.FallbackRendering{
			Subject: "/tmp/io/cmd/oh/output/region.go",
			Note:    "100-214",
		},
	}, nil, nil).WithHostPathAliases(roots)
	label.PathRoots = roots
	rendered := label.Render()

	if got := link.Plain(rendered); got != "read <s>/io/cmd/oh/output/region.go 100-214" {
		t.Errorf("visible call is %q", got)
	}
	wantAddress := "file://" + filepath.ToSlash(path) + "#100"
	if !strings.Contains(rendered, "\x1b]8;;"+wantAddress+"\x1b\\") {
		t.Errorf("read URI does not target the host scratch path in %q", rendered)
	}
}

func TestWhatQualifiesTheArgumentsIsCutFirst(t *testing.T) {
	check(t, 21, "grep", "hello", "in intern…")
	check(t, 12, "grep", "hello", "…")
}

func TestWhatQualifiesTheArgumentsGoesBeforeTheArgumentsAreCut(t *testing.T) {
	check(t, 10, "grep", "hello", "")
	check(t, 9, "grep", "hel…", "")
}

func TestTheNameIsTheLastToGo(t *testing.T) {
	check(t, 5, "grep", "", "")
	check(t, 3, "gr…", "", "")
	check(t, 1, "…", "", "")
	check(t, 0, "", "", "")
}

func TestALabelWithNothingQualifyingItIsUnaffected(t *testing.T) {
	elidedLabel, _ := call.Label{Name: "ls", Subject: "internal"}.Elide(80).(call.Label)

	if elidedLabel.Name != "ls" || elidedLabel.Subject != "internal" || elidedLabel.Qualifier != "" {
		t.Errorf("expected the label to stand, got %q %q %q", elidedLabel.Name, elidedLabel.Subject, elidedLabel.Qualifier)
	}
}

func TestALabelIsCutToTheCellsItHasRatherThanTheCharacters(t *testing.T) {
	elidedLabel, _ := call.Label{Name: "read", Subject: "日本語です"}.Elide(11).(call.Label)

	if elidedLabel.Name != "read" {
		t.Errorf("expected the name to survive, got %q", elidedLabel.Name)
	}

	if elidedLabel.Subject != "日本…" {
		t.Errorf("expected two characters and an ellipsis, got %q", elidedLabel.Subject)
	}

	if got := style.Width(elidedLabel.Name + " " + elidedLabel.Subject); got != 10 {
		t.Errorf("expected the label to measure 10 cells, got %d", got)
	}
}

func TestCallNamesAreDrawnFromTheTable(t *testing.T) {
	for name, test := range map[string]struct {
		eventName string
		want      call.Label
	}{
		"shell":    {eventName: "bash", want: call.Label{Name: "$", NameStyle: style.Shell}},
		"lookup":   {eventName: "lookup", want: call.Label{Name: "lookup", NameStyle: style.Lookup}},
		"fetch":    {eventName: "fetch", want: call.Label{Name: "fetch", NameStyle: style.Network}},
		"ordinary": {eventName: "grep", want: call.Label{Name: "grep"}},
	} {
		t.Run(name, func(t *testing.T) {
			label := call.LabelFor(agent.Event{Name: test.eventName}, nil, nil)

			if got, want := label.Render(), test.want.Render(); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestAContinuedShellCallUsesTheShellLabel(t *testing.T) {
	event := agent.Event{
		Name: "job",
		FallbackRendering: agent.FallbackRendering{
			Subject: "check",
			Continuation: []tool.CallRendering{{
				Name:     "bash",
				Subject:  "just check",
				Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
			}},
		},
	}

	label := call.LabelFor(event, nil, nil)
	if len(label.Continuation) != 1 || label.Continuation[0].Name != "$" || label.Continuation[0].NameStyle == nil {
		t.Fatalf("got %#v, want the ordinary shell label as the continuation", label.Continuation)
	}
}

func TestALabelCarriesTheTimeTheCallGaveItself(t *testing.T) {
	waitEvent := agent.Event{
		Name:      "job",
		Arguments: `{"action":"wait","name":"check","wait_seconds":20}`,
	}
	getTool := func(string) (tool.Tool, bool) {
		return job.New(jobs.New(nil), nil, nil, false), true
	}

	if got := call.LabelFor(waitEvent, getTool, nil).TimeLimit; got != 20*time.Second {
		t.Errorf("got %s, want the wait to declare the time it gave itself", got)
	}

	statusEvent := agent.Event{Name: "job", Arguments: `{"action":"status","name":"check"}`}
	if got := call.LabelFor(statusEvent, getTool, nil).TimeLimit; got != 0 {
		t.Errorf("got %s, want a call that waits for nothing to declare no bound", got)
	}
}

func TestOnlyAShellCommandOrAFailureSaysWhatItReturned(t *testing.T) {
	for name, test := range map[string]struct {
		event agent.Event
		want  string
	}{
		"shell": {
			event: agent.Event{Name: "bash", Status: agent.SuccessStatus, Text: "hello"},
			want:  "hello",
		},
		"failure": {
			event: agent.Event{Name: "read", Status: agent.ErrorStatus, Text: "no such file"},
			want:  "no such file",
		},
		"ordinary": {
			event: agent.Event{Name: "read", Status: agent.SuccessStatus, Text: "package one"},
			want:  "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := call.Summary(test.event); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestASkillReadIsDrawnAsALoad(t *testing.T) {
	label := call.LabelFor(agent.Event{
		Name:              "read",
		FallbackRendering: agent.FallbackRendering{Subject: "/skills/guard-basics/SKILL.md"},
	}, nil, nil)

	want := call.Label{
		Name:        "load",
		Subject:     "/skills/guard-basics/SKILL.md",
		NameStyle:   style.Skill,
		Accent:      "guard-basics",
		AccentStyle: style.Skill,
	}

	if got := label.Render(); got != want.Render() {
		t.Errorf("got %q, want %q", got, want.Render())
	}
}
