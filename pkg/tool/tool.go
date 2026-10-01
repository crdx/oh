package tool

import (
	"context"
	"encoding/json"
	"time"

	"crdx.org/oh/internal/util/strutil"
)

type Tool interface {
	Name() string
	Description() string
	Schema() Schema
	Revision() string
	Concurrent() bool
	ReadOnly() bool
	StateKey() string
	Render(arguments string) (CallRendering, bool)
	Parse(arguments string) (ToolCall, error)
	Restore(state json.RawMessage) error
}

type SuccessMarker interface {
	MarksSuccess() bool
}

func MarksSuccess(subject Tool) bool {
	marker, isMarker := subject.(SuccessMarker)
	return isMarker && marker.MarksSuccess()
}

type ToolCall interface {
	Rendering() CallRendering
	TimeLimit() time.Duration
	Exec(ctx context.Context) (ToolCallResult, error)
}

type CallRendering struct {
	Kind         string          `json:"kind,omitempty"`
	Subject      string          `json:"render,omitempty"`
	Qualifier    string          `json:"detail,omitempty"`
	PathLine     string          `json:"path_line,omitempty"`
	Emphasis     Emphasis        `json:"emphasis,omitzero"`
	Continuation []CallRendering `json:"continuation,omitempty"`
	ShowOutput   bool            `json:"show_output,omitempty"`
}

func (self CallRendering) HasArguments() bool {
	return self.Subject != "" || self.Qualifier != "" || len(self.Continuation) > 0
}

type ToolCallResult struct {
	Output    string
	Image     Image
	Metrics   ToolCallMetrics
	State     json.RawMessage
	FileLines FileLines
}

type FileLines struct {
	First int
	Total int
}

type ToolCallMetrics struct {
	Kind            string        `json:"kind,omitempty"`
	CPUTime         time.Duration `json:"cpu_time,omitempty"`
	PeakMemory      uint64        `json:"peak_memory,omitempty"`
	Lines           int64         `json:"lines,omitempty"`
	Bytes           int64         `json:"bytes,omitempty"`
	TotalBytes      int64         `json:"total_bytes,omitempty"`
	EstimatedTokens int64         `json:"estimated_tokens,omitempty"`
	AddedLines      int64         `json:"added,omitempty"`
	RemovedLines    int64         `json:"removed,omitempty"`
	IsTruncated     bool          `json:"truncated,omitempty"`
}

const (
	MetricOutput    = "output"
	MetricResources = "resources"
	MetricRead      = "read"
	MetricList      = "list"
	MetricImage     = "image"
	MetricWrite     = "write"
	MetricDiff      = "diff"
	MetricSearch    = "search"
)

func GetMetrics(output string) ToolCallMetrics {
	bytes := int64(len(output))

	return ToolCallMetrics{
		Kind:  MetricOutput,
		Lines: int64(len(strutil.Lines(output))),
		Bytes: bytes,
	}
}

type EmphasisKind string

const (
	EmphasisSyntax EmphasisKind = "syntax"
	EmphasisFocus  EmphasisKind = "focus"
)

type Emphasis struct {
	Kind   EmphasisKind `json:"kind"`
	Value  string       `json:"value"`
	Source string       `json:"-"`
}

type Renderer[T any] func(args T) CallRendering

type Validator[T any] func(args T) error

type Decoder[T any] func(arguments string) (T, error)

type ResultExecutor[T any] func(ctx context.Context, args T) (ToolCallResult, error)

type Restorer func(state json.RawMessage) error

type Executor[T any] func(ctx context.Context, args T) (string, error)

type MetricsExecutor[T any] func(ctx context.Context, args T) (string, ToolCallMetrics, error)
