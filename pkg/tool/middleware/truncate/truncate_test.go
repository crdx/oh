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

func newLimit(t *testing.T, bytes int) *truncate.Limit {
	t.Helper()

	limit := truncate.NewLimit(bytes)
	limit.SaveOverflowWith(newSaver(t, t.TempDir()))
	return limit
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
		func(args Args) (string, string) { return "generate", "" },
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
		func(Args) (string, string) { return "measured", "" },
	).Exec(func(context.Context, Args) (string, tool.ToolCallMetrics, error) {
		return "done", tool.ToolCallMetrics{Kind: tool.MetricRead, Lines: 3, Bytes: 12}, nil
	})

	call, err := truncate.Tool(subject, newLimit(t, limitBytes)).Parse(`{}`)
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
		func(Args) (string, string) { return "measured", "" },
	).Exec(func(context.Context, Args) (string, tool.ToolCallMetrics, error) {
		return whole, tool.ToolCallMetrics{Kind: tool.MetricResources}, nil
	})

	call, err := truncate.Tool(subject, newLimit(t, limitBytes)).Parse(`{}`)
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
		func(Args) (string, string) { return "image", "" },
	).Run(func(context.Context, Args) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{
			Output: "image/png image",
			Image:  tool.Image{MediaType: "image/png", Data: []byte{1}},
		}, nil
	})

	call, err := truncate.Tool(subject, newLimit(t, limitBytes)).Parse(`{}`)
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
	output := truncate.Output("hello\n", newLimit(t, limitBytes))

	if output != "hello\n" {
		t.Errorf("expected the output untouched, got %q", output)
	}
}

func TestTheLimitItIsGivenIsTheOneItCutsAt(t *testing.T) {
	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole, newLimit(t, 2*1024))

	if !strings.Contains(output, "truncated at 1.99K of 58.6K") {
		t.Errorf("expected the cut at the limit it was given, got %q", output)
	}
}

func TestOutputTooBigIsCutAndSaved(t *testing.T) {
	directory := t.TempDir()
	limit := truncate.NewLimit(limitBytes)
	limit.SaveOverflowWith(newSaver(t, directory))

	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole, limit)

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

	if !strings.Contains(output, "truncated at 12K of 58.6K") {
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

	output := truncate.Output(whole, truncate.NewLimit(limitBytes))

	if !strings.HasSuffix(output, "[truncated at 12K of 58.6K]") {
		t.Errorf("expected the notice to name no file, got %q", output)
	}
}

func TestOutputTooBigSaysSoWhenItCouldNotBeSaved(t *testing.T) {
	limit := truncate.NewLimit(limitBytes)
	limit.SaveOverflowWith(func(string) (string, error) {
		return "", errors.New("the drops directory disappeared")
	})

	output := truncate.Output(strings.Repeat("a line of text\n", 4000), limit)

	if !strings.HasSuffix(output, "the rest could not be saved: the drops directory disappeared]") {
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
		func(Args) (string, string) { return "read", "" },
	).Run(func(context.Context, Args) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{Output: output, FileLines: lines}, nil
	})
}

func TestLinesOfAFileCutShortNameTheOffsetToContinueWith(t *testing.T) {
	directory := t.TempDir()
	limit := truncate.NewLimit(limitBytes)
	limit.SaveOverflowWith(newSaver(t, directory))

	var whole strings.Builder
	for number := 101; number <= 2000; number++ {
		fmt.Fprintf(&whole, "line %d\n", number)
	}
	subject := truncate.Tool(fileLinesTool(whole.String(), tool.FileLines{First: 101, Total: 2000}), limit)

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
	limit.SaveOverflowWith(newSaver(t, directory))

	whole := strings.Repeat("x", 2*limitBytes) + "\nshort\n"
	subject := truncate.Tool(fileLinesTool(whole, tool.FileLines{First: 1, Total: 2}), limit)

	output := exec(t, subject, `{}`)

	saved, err := filepath.Glob(filepath.Join(directory, "output-*.txt"))
	if err != nil || len(saved) != 1 {
		t.Fatalf("expected the whole of it saved once, got %v and %v", saved, err)
	}
	if !strings.HasSuffix(output, "full output in "+saved[0]+"]") {
		t.Errorf("expected the notice to name the file it saved, got %q", output[len(output)-120:])
	}
}

func TestAWrappedToolKeepsItsSyntaxHighlighting(t *testing.T) {
	subject := buildTool(newToolBuilder(t).Syntax("bash"))
	call, err := truncate.Tool(subject, newLimit(t, limitBytes)).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if call.Emphasis() != want {
		t.Errorf("expected the emphasis to survive, got %T", call)
	}
}

func TestAWrappedToolKeepsItsFocusedRendering(t *testing.T) {
	subject := buildTool(newToolBuilder(t).Focuses(func(tool.ToolCall) string { return "generate" }))
	call, err := truncate.Tool(subject, newLimit(t, limitBytes)).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "generate"}
	if call.Emphasis() != want {
		t.Errorf("expected the focus to survive, got %T", call)
	}
}

func TestAWrappedToolIsCapped(t *testing.T) {
	subject := truncate.Tool(buildTool(newToolBuilder(t)), newLimit(t, limitBytes))

	if subject.Name() != "generate" {
		t.Errorf("expected the name to survive, got %q", subject.Name())
	}

	small := exec(t, subject, `{"size":2}`)
	if small != "a line of text\na line of text\n" {
		t.Errorf("expected a small reply untouched, got %q", small)
	}

	big := exec(t, subject, `{"size":4000}`)
	if !strings.Contains(big, "truncated at") {
		t.Error("expected an oversized reply to be cut")
	}
}

func TestAnUnwrappedToolIsNotCapped(t *testing.T) {
	output := exec(t, buildTool(newToolBuilder(t)), `{"size":4000}`)

	if strings.Contains(output, "truncated at") {
		t.Error("expected an unwrapped tool to hand back everything")
	}
}

func TestToolsWrapsEveryTool(t *testing.T) {
	wrappedTools := truncate.Tools([]tool.Tool{
		buildTool(newToolBuilder(t)),
		buildTool(newToolBuilder(t)),
	}, newLimit(t, limitBytes))

	if len(wrappedTools) != 2 {
		t.Fatalf("expected both tools back, got %d", len(wrappedTools))
	}

	for _, subject := range wrappedTools {
		if !strings.Contains(exec(t, subject, `{"size":4000}`), "truncated at") {
			t.Error("expected every tool to be capped")
		}
	}
}

func TestEachCallKeepsTheOutputLimitItStartedWith(t *testing.T) {
	limit := newLimit(t, 1024)
	subject := truncate.Tools([]tool.Tool{buildTool(newToolBuilder(t))}, limit)[0]

	started, err := subject.Parse(`{"size":400}`)
	if err != nil {
		t.Fatal(err)
	}
	limit.Replace(limitBytes)

	result, err := started.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "truncated at") {
		t.Error("the started call lost its original limit")
	}
	if output := exec(t, subject, `{"size":400}`); strings.Contains(output, "truncated at") {
		t.Error("a later call did not use the replacement limit")
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
		}), newLimit(t, limitBytes))

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
