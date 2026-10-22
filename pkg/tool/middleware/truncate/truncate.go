package truncate

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
)

type Saver func(output string) (string, error)

type Limit struct {
	bytes atomic.Int64
}

func NewLimit(bytes int) *Limit {
	limit := &Limit{}
	limit.Replace(bytes)
	return limit
}

func (self *Limit) GetBytes() int {
	return int(self.bytes.Load())
}

func (self *Limit) Replace(bytes int) {
	self.bytes.Store(int64(bytes))
}

func Tools(subjects []tool.Tool, limit *Limit, save Saver) []tool.Tool {
	wrappedTools := make([]tool.Tool, len(subjects))

	for i, subject := range subjects {
		wrappedTools[i] = Tool(subject, limit, save)
	}

	return wrappedTools
}

func Tool(inner tool.Tool, limit *Limit, save Saver) tool.Tool {
	return truncatedTool{Tool: inner, limit: limit, save: save}
}

type truncatedTool struct {
	tool.Tool

	limit *Limit
	save  Saver
}

func (self truncatedTool) CompatibleWith(revision string) bool {
	return tool.AcceptsRevision(self.Tool, revision)
}

func (self truncatedTool) MarksSuccess() bool {
	return tool.MarksSuccess(self.Tool)
}

func (self truncatedTool) Parse(arguments string) (tool.ToolCall, error) {
	return self.truncated(self.Tool.Parse(arguments))
}

func (self truncatedTool) ParseUnder(schema tool.Schema, arguments string) (tool.ToolCall, error) {
	return self.truncated(tool.ParseUnder(self.Tool, schema, arguments))
}

func (self truncatedTool) RenderUnder(schema tool.Schema, arguments string) (tool.CallRendering, bool) {
	return tool.RenderUnder(self.Tool, schema, arguments)
}

func (self truncatedTool) truncated(call tool.ToolCall, err error) (tool.ToolCall, error) {
	if err != nil {
		return nil, err
	}

	return truncatedToolCall{ToolCall: call, limit: self.limit.GetBytes(), save: self.save}, nil
}

type truncatedToolCall struct {
	tool.ToolCall

	limit int
	save  Saver
}

func (self truncatedToolCall) Exec(ctx context.Context) (tool.ToolCallResult, error) {
	result, err := self.ToolCall.Exec(ctx)
	cappedOutput, returnedBytes, totalBytes := outputWithSizes(result.Output, self.limit, self.save, result.FileLines)
	result.Output = cappedOutput

	if result.Metrics.Kind == tool.MetricResources || returnedBytes < totalBytes {
		result.Metrics.Bytes = int64(returnedBytes)
		result.Metrics.TotalBytes = int64(totalBytes)
		result.Metrics.IsTruncated = result.Metrics.IsTruncated || returnedBytes < totalBytes
	}

	return result, err
}

func Output(output string, limit *Limit, save Saver) string {
	cappedOutput, _, _ := outputWithSizes(output, limit.GetBytes(), save, tool.FileLines{})
	return cappedOutput
}

const (
	cutSlack  = 256
	headShare = 3
)

func outputWithSizes(output string, limit int, save Saver, lines tool.FileLines) (string, int, int) {
	if len(output) <= limit {
		return output, len(output), len(output)
	}

	if lines.First > 0 {
		end := limit
		if newline := strings.LastIndexByte(output[:limit], '\n'); newline > 0 {
			end = newline
		}
		if output[end] == '\n' {
			last := lines.First + strings.Count(output[:end], "\n")
			return fmt.Sprintf(
				"%s\n\n[truncated at line %d of %d; continue with offset %d]",
				output[:end], last, lines.Total, last+1,
			), end, len(output)
		}
	}

	path, saveErr := "", error(nil)
	if save != nil {
		path, saveErr = save(output)
	}
	note := func(headBytes int, tailBytes int) string {
		sizes := fmt.Sprintf(
			"truncated: kept the first %s and the last %s of %s",
			util.FormatBytes(headBytes, 3), util.FormatBytes(tailBytes, 3), util.FormatBytes(len(output), 3),
		)
		switch {
		case save == nil:
			return "\n\n[" + sizes + "]\n\n"
		case saveErr != nil:
			return fmt.Sprintf("\n\n[%s; the rest could not be saved: %v]\n\n", sizes, saveErr)
		default:
			return "\n\n[" + sizes + "; full output in " + path + "]\n\n"
		}
	}

	room := max(limit-len(note(limit, limit))-min(cutSlack, limit/8), limit/2)
	headEnd := room / headShare
	if newline := strings.LastIndexByte(output[:headEnd], '\n'); newline > 0 {
		headEnd = newline + 1
	} else {
		for headEnd > 0 && !utf8.RuneStart(output[headEnd]) {
			headEnd--
		}
	}
	tailStart := len(output) - (room - headEnd)
	if newline := strings.IndexByte(output[tailStart:], '\n'); newline >= 0 && tailStart+newline+1 < len(output) {
		tailStart += newline + 1
	} else {
		for tailStart < len(output) && !utf8.RuneStart(output[tailStart]) {
			tailStart++
		}
	}

	head, tail := strings.TrimRight(output[:headEnd], "\n"), output[tailStart:]
	return head + note(len(head), len(tail)) + tail, len(head) + len(tail), len(output)
}
