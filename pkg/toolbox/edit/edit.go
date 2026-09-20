package edit

import (
	"context"
	"errors"
	"strings"

	"crdx.org/oh/internal/file"

	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/tool"
)

type Args struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "edit",
			Description: "replace an exact string in a file, which must be read first and appear exactly once",
			Schema: tool.Schema{
				tool.String("path", "file"),
				tool.String("old_text", "exact text to replace, including whitespace"),
				tool.String("new_text", "replacement text"),
			},
		},
		Describe,
	).FocusPath().Exec(func(_ context.Context, args Args) (string, tool.ToolCallMetrics, error) {
		return exec(root, snapshots, args)
	})
}

func Describe(args Args) (string, string) {
	return args.Path, ""
}

func exec(root *file.Root, snapshots *file.Snapshots, args Args) (string, tool.ToolCallMetrics, error) {
	switch {
	case args.Path == "":
		return "", tool.ToolCallMetrics{}, errors.New("path is required")
	case args.OldText == "":
		return "", tool.ToolCallMetrics{}, errors.New("old_text is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	if err := root.RefuseWrite(name); err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}
	if err := snapshots.Check(root, name, data); err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	content := string(data)

	switch strings.Count(content, args.OldText) {
	case 0:
		return "", tool.ToolCallMetrics{}, errors.New("old_text not found")
	case 1:
	default:
		return "", tool.ToolCallMetrics{}, errors.New(
			"old_text is not unique; include more context",
		)
	}

	info, err := root.Stat(name)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	updatedContent := strings.Replace(content, args.OldText, args.NewText, 1)

	updatedData := []byte(updatedContent)
	if err := root.WriteFile(name, updatedData, info.Mode()); err != nil {
		return "", tool.ToolCallMetrics{}, err
	}
	snapshots.Record(root, name, updatedData)

	addedLines, removedLines := changedLines(args.OldText, args.NewText)
	metrics := tool.ToolCallMetrics{Kind: tool.MetricDiff, AddedLines: addedLines, RemovedLines: removedLines}
	return "edited", metrics, nil
}

func changedLines(before string, after string) (int64, int64) {
	oldLines := strutil.Lines(before)
	newLines := strutil.Lines(after)

	for len(oldLines) > 0 && len(newLines) > 0 && oldLines[0] == newLines[0] {
		oldLines, newLines = oldLines[1:], newLines[1:]
	}
	for len(oldLines) > 0 && len(newLines) > 0 &&
		oldLines[len(oldLines)-1] == newLines[len(newLines)-1] {
		oldLines, newLines = oldLines[:len(oldLines)-1], newLines[:len(newLines)-1]
	}

	return int64(len(newLines)), int64(len(oldLines))
}
