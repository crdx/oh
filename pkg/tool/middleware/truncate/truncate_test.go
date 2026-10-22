package truncate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/truncate"
)

const limitBytes = 12 * 1024

func temporarySaver(t *testing.T) truncate.Saver {
	t.Helper()

	return newSaver(t, t.TempDir())
}

func newSaver(t *testing.T, directory string) truncate.Saver {
	t.Helper()

	return func(output string) (string, error) {
		file, err := os.CreateTemp(directory, "output-*.txt")
		if err != nil {
			return "", err
		}
		defer func() { _ = file.Close() }()

		if _, err := file.WriteString(output); err != nil {
			return "", err
		}
		return file.Name(), nil
	}
}

type Args struct {
	Size int `json:"size"`
}

func newToolBuilder(t *testing.T) tool.Builder[Args] {
	t.Helper()

	return tool.Implement(
		tool.Definition{
			Name:        "generate",
			Description: "generate output",
			Schema:      tool.Schema{tool.Integer("size", "how many lines to generate")},
		},
		func(args Args) tool.CallRendering {
			return tool.CallRendering{Subject: "generate", Qualifier: ""}
		},
	)
}

func buildTool(builder tool.Builder[Args]) tool.Tool {
	return builder.Plain(func(_ context.Context, args Args) (string, error) {
		return strings.Repeat("a line of text\n", args.Size), nil
	})
}

func exec(t *testing.T, subject tool.Tool, arguments string) string {
	t.Helper()

	call, err := subject.Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return result.Output
}

func TestStatisticsPassThroughTheOutputCap(t *testing.T) {
	subject := tool.Implement(
		tool.Definition{
			Name:        "measured",
			Description: "measure something",
			Schema:      tool.Schema{},
		},
		func(Args) tool.CallRendering {
			return tool.CallRendering{Subject: "measured", Qualifier: ""}
		},
	).Exec(func(context.Context, Args) (string, tool.ToolCallMetrics, error) {
		return "done", tool.ToolCallMetrics{Kind: tool.MetricRead, Lines: 3, Bytes: 12}, nil
	})

	call, err := truncate.Tool(subject, truncate.NewLimit(limitBytes), temporarySaver(t)).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if result.Metrics.Lines != 3 || result.Metrics.Bytes != 12 {
		t.Errorf("got %+v", result.Metrics)
	}
}

func TestTruncatedStatisticsReportReturnedAndTotalOutput(t *testing.T) {
	whole := strings.Repeat("a line of text\n", 4000)
	subject := tool.Implement(
		tool.Definition{
			Name:        "measured",
			Description: "measure something",
			Schema:      tool.Schema{},
		},
		func(Args) tool.CallRendering {
			return tool.CallRendering{Subject: "measured", Qualifier: ""}
		},
	).Exec(func(context.Context, Args) (string, tool.ToolCallMetrics, error) {
		return whole, tool.ToolCallMetrics{Kind: tool.MetricResources}, nil
	})

	call, err := truncate.Tool(subject, truncate.NewLimit(limitBytes), temporarySaver(t)).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	statistics := result.Metrics
	if statistics.Bytes <= 0 || statistics.Bytes > limitBytes || statistics.TotalBytes != int64(len(whole)) || !statistics.IsTruncated {
		t.Errorf("expected returned and total output statistics, got %+v", statistics)
	}
}

func TestAnAttachedImagePassesThroughTheOutputCap(t *testing.T) {
	subject := tool.Implement(
		tool.Definition{
			Name:        "image",
			Description: "return an image",
			Schema:      tool.Schema{},
		},
		func(Args) tool.CallRendering {
			return tool.CallRendering{Subject: "image", Qualifier: ""}
		},
	).Run(func(context.Context, Args) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{
			Output: "image/png image",
			Image:  tool.Image{MediaType: "image/png", Data: []byte{1}},
		}, nil
	})

	call, err := truncate.Tool(subject, truncate.NewLimit(limitBytes), temporarySaver(t)).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if result.Image.MediaType != "image/png" || len(result.Image.Data) != 1 {
		t.Errorf("expected the image to pass through, got %+v", result.Image)
	}
}

func TestOutputThatFitsIsLeftAlone(t *testing.T) {
	output := truncate.Output("hello\n", truncate.NewLimit(limitBytes), temporarySaver(t))

	if output != "hello\n" {
		t.Errorf("expected the output untouched, got %q", output)
	}
}

func TestTheLimitItIsGivenIsTheOneItCutsAt(t *testing.T) {
	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole, truncate.NewLimit(2*1024), temporarySaver(t))

	if !strings.Contains(output, "[truncated: kept the first 539B and the last 1.07K of 58.6K;") {
		t.Errorf("expected the cut at the limit it was given, got %q", output)
	}
}

func TestOutputTooBigIsCutAndSaved(t *testing.T) {
	directory := t.TempDir()
	limit := truncate.NewLimit(limitBytes)
	save := newSaver(t, directory)

	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole, limit, save)

	if len(output) > limitBytes+300 {
		t.Errorf("expected the output to be capped, got %d bytes", len(output))
	}

	if !strings.HasSuffix(strings.SplitN(output, "\n\n[", 2)[0], "a line of text") {
		t.Error("expected the cut to fall on a line boundary")
	}

	saved, err := filepath.Glob(filepath.Join(directory, "output-*.txt"))
	if err != nil || len(saved) != 1 {
		t.Fatalf("expected the whole of it saved once, got %v and %v", saved, err)
	}

	if !strings.Contains(output, "[truncated: kept the first 3.87K and the last 7.73K of 58.6K;") {
		t.Errorf("expected compact byte sizes in the notice, got %q", output)
	}
	if !strings.Contains(output, saved[0]) {
		t.Errorf("expected the notice to name the file it saved, got %q", output)
	}

	savedOutput, err := os.ReadFile(saved[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(savedOutput) != whole {
		t.Errorf("expected the whole output to be saved, got %d of %d bytes", len(savedOutput), len(whole))
	}
}

func TestOutputTooBigNamesNoFileWhenNothingSavesIt(t *testing.T) {
	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole, truncate.NewLimit(limitBytes), nil)

	if !strings.Contains(output, "[truncated: kept the first 3.9K and the last 7.79K of 58.6K]") {
		t.Errorf("expected the notice to name no file, got %q", output)
	}
}

func TestOutputTooBigSaysSoWhenItCouldNotBeSaved(t *testing.T) {
	failingSave := func(string) (string, error) {
		return "", errors.New("the drops directory disappeared")
	}

	output := truncate.Output(strings.Repeat("a line of text\n", 4000), truncate.NewLimit(limitBytes), failingSave)

	if !strings.Contains(output, "the rest could not be saved: the drops directory disappeared]") {
		t.Errorf("expected the notice to name the failure, got %q", output)
	}
}

func fileLinesTool(output string, lines tool.FileLines) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "read lines of a file",
			Schema:      tool.Schema{},
		},
		func(Args) tool.CallRendering { return tool.CallRendering{Subject: "read"} },
	).Run(func(context.Context, Args) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{Output: output, FileLines: lines}, nil
	})
}

func TestLinesOfAFileCutShortNameTheOffsetToContinueWith(t *testing.T) {
	directory := t.TempDir()
	limit := truncate.NewLimit(limitBytes)
	save := newSaver(t, directory)

	var whole strings.Builder
	for number := 101; number <= 2000; number++ {
		fmt.Fprintf(&whole, "line %d\n", number)
	}
	subject := truncate.Tool(fileLinesTool(whole.String(), tool.FileLines{First: 101, Total: 2000}), limit, save)

	output := exec(t, subject, `{}`)

	shown, notice, isCut := strings.Cut(output, "\n\n[")
	if !isCut {
		t.Fatalf("expected the output to be cut, got %d bytes", len(output))
	}
	var last, total, next int
	if _, err := fmt.Sscanf(notice, "truncated at line %d of %d; continue with offset %d]", &last, &total, &next); err != nil {
		t.Fatalf("expected the notice to name lines, got %q: %v", notice, err)
	}
	if !strings.HasSuffix(shown, fmt.Sprintf("\nline %d", last)) {
		t.Errorf("expected line %d to be the last shown, got %q", last, shown[max(len(shown)-20, 0):])
	}
	if total != 2000 || next != last+1 {
		t.Errorf("got line %d of %d continuing at %d", last, total, next)
	}

	saved, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil || len(saved) != 0 {
		t.Errorf("expected nothing saved for lines that can be read again, got %v and %v", saved, err)
	}
}

func TestALineOfAFileTooLongToShowIsSaved(t *testing.T) {
	directory := t.TempDir()
	limit := truncate.NewLimit(limitBytes)
	save := newSaver(t, directory)

	whole := strings.Repeat("x", 2*limitBytes) + "\nshort\n"
	subject := truncate.Tool(fileLinesTool(whole, tool.FileLines{First: 1, Total: 2}), limit, save)

	output := exec(t, subject, `{}`)

	saved, err := filepath.Glob(filepath.Join(directory, "output-*.txt"))
	if err != nil || len(saved) != 1 {
		t.Fatalf("expected the whole of it saved once, got %v and %v", saved, err)
	}
	if !strings.Contains(output, "full output in "+saved[0]+"]") {
		t.Errorf("expected the notice to name the file it saved, got %q", output[len(output)-120:])
	}
}

func TestAWrappedToolKeepsItsSyntaxHighlighting(t *testing.T) {
	subject := buildTool(newToolBuilder(t).Syntax("bash"))
	call, err := truncate.Tool(subject, truncate.NewLimit(limitBytes), temporarySaver(t)).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if call.Rendering().Emphasis != want {
		t.Errorf("expected the emphasis to survive, got %T", call)
	}
}

func TestAWrappedToolKeepsItsFocusedRendering(t *testing.T) {
	subject := buildTool(newToolBuilder(t).Focuses(func(tool.CallRendering) string { return "generate" }))
	call, err := truncate.Tool(subject, truncate.NewLimit(limitBytes), temporarySaver(t)).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "generate"}
	if call.Rendering().Emphasis != want {
		t.Errorf("expected the focus to survive, got %T", call)
	}
}

func TestAWrappedToolIsCapped(t *testing.T) {
	subject := truncate.Tool(buildTool(newToolBuilder(t)), truncate.NewLimit(limitBytes), temporarySaver(t))

	if subject.Name() != "generate" {
		t.Errorf("expected the name to survive, got %q", subject.Name())
	}

	small := exec(t, subject, `{"size":2}`)
	if small != "a line of text\na line of text\n" {
		t.Errorf("expected a small reply untouched, got %q", small)
	}

	big := exec(t, subject, `{"size":4000}`)
	if !strings.Contains(big, "[truncated") {
		t.Error("expected an oversized reply to be cut")
	}
}

func TestAnUnwrappedToolIsNotCapped(t *testing.T) {
	output := exec(t, buildTool(newToolBuilder(t)), `{"size":4000}`)

	if strings.Contains(output, "[truncated") {
		t.Error("expected an unwrapped tool to hand back everything")
	}
}

func TestToolsWrapsEveryTool(t *testing.T) {
	wrappedTools := truncate.Tools([]tool.Tool{
		buildTool(newToolBuilder(t)),
		buildTool(newToolBuilder(t)),
	}, truncate.NewLimit(limitBytes), temporarySaver(t))

	if len(wrappedTools) != 2 {
		t.Fatalf("expected both tools back, got %d", len(wrappedTools))
	}

	for _, subject := range wrappedTools {
		if !strings.Contains(exec(t, subject, `{"size":4000}`), "[truncated") {
			t.Error("expected every tool to be capped")
		}
	}
}

func TestEachCallKeepsTheOutputLimitItStartedWith(t *testing.T) {
	limit := truncate.NewLimit(1024)
	subject := truncate.Tools([]tool.Tool{buildTool(newToolBuilder(t))}, limit, temporarySaver(t))[0]

	started, err := subject.Parse(`{"size":400}`)
	if err != nil {
		t.Fatal(err)
	}
	limit.Replace(limitBytes)

	result, err := started.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "[truncated") {
		t.Error("the started call lost its original limit")
	}
	if output := exec(t, subject, `{"size":400}`); strings.Contains(output, "[truncated") {
		t.Error("a later call did not use the replacement limit")
	}
}

const sharedLimitBytes = 1024

func TestToolsSharingALimitFollowItsSizeButSaveWhereEachIsTold(t *testing.T) {
	parentDirectory, childDirectory := t.TempDir(), t.TempDir()
	limit := truncate.NewLimit(sharedLimitBytes)
	truncate.Tool(buildTool(newToolBuilder(t)), limit, newSaver(t, parentDirectory))
	child := truncate.Tool(buildTool(newToolBuilder(t)), limit, newSaver(t, childDirectory))

	if output := exec(t, child, `{"size":400}`); !strings.Contains(output, "full output in "+childDirectory) {
		t.Errorf("the child saved its overflow elsewhere: %q", output)
	}
	if saved, _ := os.ReadDir(parentDirectory); len(saved) != 0 {
		t.Errorf("the parent saved %d outputs it never produced", len(saved))
	}
	limit.Replace(limitBytes)
	if output := exec(t, child, `{"size":400}`); strings.Contains(output, "[truncated") {
		t.Error("the child missed the shared limit's new size")
	}
}

func TestAWrappedToolKeepsOwningItsDurableState(t *testing.T) {
	restored := ""
	subject := truncate.Tool(newToolBuilder(t).
		State("generated", func(state json.RawMessage) error {
			restored = string(state)
			return nil
		}).
		Run(func(context.Context, Args) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "done", State: json.RawMessage(`{"lines":2}`)}, nil
		}), truncate.NewLimit(limitBytes), temporarySaver(t))

	if key := subject.StateKey(); key != "generated" {
		t.Errorf("the wrapped tool owns %q", key)
	}

	call, err := subject.Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.State) != `{"lines":2}` {
		t.Errorf("the cap swallowed the state: %s", result.State)
	}

	if err := subject.Restore(result.State); err != nil || restored != `{"lines":2}` {
		t.Errorf("the wrapped tool restored %q: %v", restored, err)
	}
}

func TestTheCapMarksSuccessAsTheToolItWrapsDoes(t *testing.T) {
	limit := truncate.NewLimit(1024)

	if !tool.MarksSuccess(truncate.Tool(buildTool(newToolBuilder(t).MarksSuccess()), limit, nil)) {
		t.Error("expected the cap to mark success when the tool it wraps does")
	}
	if tool.MarksSuccess(truncate.Tool(buildTool(newToolBuilder(t)), limit, nil)) {
		t.Error("expected the cap to leave success unmarked when the tool it wraps does")
	}
}

func TestAFrozenSchemaReachesTheToolItWraps(t *testing.T) {
	wrapped := truncate.Tool(buildTool(newToolBuilder(t)), truncate.NewLimit(limitBytes), temporarySaver(t))
	frozen := tool.WithSnapshot(wrapped, tool.Snapshot{
		Definition: tool.Definition{
			Name:   "generate",
			Schema: tool.Schema{tool.Integer("size", "how many lines to generate").Optional()},
		},
		Revision: wrapped.Revision(),
	})

	if _, err := wrapped.Parse(`{}`); err == nil {
		t.Fatal("the current schema accepted a call without its required size")
	}
	if output := exec(t, frozen, `{}`); output != "" {
		t.Errorf("got %q, want nothing generated", output)
	}
	if _, isRendered := frozen.Render(`{}`); !isRendered {
		t.Error("the frozen schema did not reach the wrapped tool's rendering")
	}
}

func TestAnOutputTooBigKeepsBothEnds(t *testing.T) {
	directory := t.TempDir()
	buffer := &tool.OutputBuffer{}
	for line := range 200000 {
		if _, err := fmt.Fprintf(buffer, "%d\n", line+1); err != nil {
			t.Fatal(err)
		}
	}
	whole := "exit(1):\n" + buffer.Report() + "note: the command failed."

	output := truncate.Output(whole, truncate.NewLimit(limitBytes), newSaver(t, directory))

	if len(output) > limitBytes {
		t.Errorf("got %d bytes, want at most %d", len(output), limitBytes)
	}
	if !strings.HasPrefix(output, "exit(1):\n[omitted 1026751 earlier output bytes]\n\n162552\n") {
		t.Errorf("got %q, want the start of the output kept", output[:60])
	}
	if !strings.HasSuffix(output, "\n199999\n200000\nnote: the command failed.") {
		t.Errorf("got %q, want the end of the output kept", output[len(output)-60:])
	}
	saved, err := filepath.Glob(filepath.Join(directory, "output-*.txt"))
	if err != nil || len(saved) != 1 {
		t.Fatalf("expected the output saved once, got %v and %v", saved, err)
	}
	if !strings.Contains(output, "\n\n[truncated: kept the first 3.86K and the last 7.74K of 256K; full output in "+saved[0]+"]\n\n") {
		t.Errorf("got %q, want one note between the ends", output)
	}
}

func TestOutputCutOnceIsNotCutAgain(t *testing.T) {
	directory := t.TempDir()
	limit := truncate.NewLimit(limitBytes)
	save := newSaver(t, directory)
	whole := strings.Repeat("a line of text\n", 4000)

	once := truncate.Output(whole, limit, save)
	twice := truncate.Output("docs: done\n"+once, limit, save)

	if twice != "docs: done\n"+once {
		t.Errorf("got %q, want the cut output left alone", twice[len(twice)-200:])
	}
	saved, err := filepath.Glob(filepath.Join(directory, "output-*.txt"))
	if err != nil || len(saved) != 1 {
		t.Errorf("got %v and %v, want the output saved once", saved, err)
	}
}
