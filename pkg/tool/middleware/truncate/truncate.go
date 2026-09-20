package truncate

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/tool"
)

type Saver func(output string) (string, error)

type Limit struct {
	bytes atomic.Int64
	save  atomic.Pointer[Saver]
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

func (self *Limit) SaveOverflowWith(save Saver) {
	self.save.Store(&save)
}

func (self *Limit) getSaver() Saver {
	if save := self.save.Load(); save != nil {
		return *save
	}

	return nil
}

func Tools(subjects []tool.Tool, limit *Limit) []tool.Tool {
	wrappedTools := make([]tool.Tool, len(subjects))

	for i, subject := range subjects {
		wrappedTools[i] = truncatedTool{Tool: subject, limit: limit}
	}

	return wrappedTools
}

func Tool(inner tool.Tool, limit *Limit) tool.Tool {
	return truncatedTool{Tool: inner, limit: limit}
}

type truncatedTool struct {
	tool.Tool

	limit *Limit
}

func (self truncatedTool) Parse(arguments string) (tool.ToolCall, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return truncatedToolCall{ToolCall: call, limit: self.limit.GetBytes(), save: self.limit.getSaver()}, nil
}

type truncatedToolCall struct {
	tool.ToolCall

	limit int
	save  Saver
}

func (self truncatedToolCall) Exec(ctx context.Context) (tool.ToolCallResult, error) {
	result, err := self.ToolCall.Exec(ctx)
	cappedOutput, returnedBytes, totalBytes := outputWithSizes(result.Output, self.limit, self.save)
	result.Output = cappedOutput

	if result.Metrics.Kind == tool.MetricResources || returnedBytes < totalBytes {
		result.Metrics.Bytes = int64(returnedBytes)
		result.Metrics.TotalBytes = int64(totalBytes)
		result.Metrics.IsTruncated = result.Metrics.IsTruncated || returnedBytes < totalBytes
	}

	return result, err
}

func Output(output string, limit *Limit) string {
	cappedOutput, _, _ := outputWithSizes(output, limit.GetBytes(), limit.getSaver())
	return cappedOutput
}

func outputWithSizes(output string, limit int, save Saver) (string, int, int) {
	if len(output) <= limit {
		return output, len(output), len(output)
	}

	end := limit

	if newline := strings.LastIndexByte(output[:limit], '\n'); newline > 0 {
		end = newline
	} else {
		for end > 0 && !utf8.RuneStart(output[end]) {
			end--
		}
	}

	if save == nil {
		return fmt.Sprintf(
			"%s\n\n[truncated at %s of %s]",
			output[:end], util.FormatBytes(end, 3), util.FormatBytes(len(output), 3),
		), end, len(output)
	}

	path, err := save(output)
	if err != nil {
		return fmt.Sprintf(
			"%s\n\n[truncated at %s of %s; the rest could not be saved: %v]",
			output[:end], util.FormatBytes(end, 3), util.FormatBytes(len(output), 3), err,
		), end, len(output)
	}

	return fmt.Sprintf(
		"%s\n\n[truncated at %s of %s; full output in %s]",
		output[:end], util.FormatBytes(end, 3), util.FormatBytes(len(output), 3), path,
	), end, len(output)
}
