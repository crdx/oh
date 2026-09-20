package find

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
)

type Args struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func New(root *file.Root) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "find",
			Description: "find files",
			Schema: tool.Schema{
				tool.String("pattern", "glob"),
				tool.String(
					"path",
					"directory (default: cwd)",
				).Optional(),
			},
		},
		Describe,
	).
		Focuses(util.SearchPath).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Exec(func(_ context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return exec(root, args)
		})
}

func Describe(args Args) (string, string) {
	return util.DescribeSearch(args.Pattern, args.Path, "")
}

func exec(root *file.Root, args Args) (string, tool.ToolCallMetrics, error) {
	if args.Pattern == "" {
		return "", tool.ToolCallMetrics{}, errors.New("pattern is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	var matches []string
	returnedBytes := int64(0)
	isTruncated := false

	err = util.Walk(root.FS(), name, func(path string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(name, path)
		if err != nil {
			return nil //nolint:nilerr // an unrelatable path is skipped, not fatal
		}

		if !util.MatchGlob(args.Pattern, relativePath) {
			return nil
		}

		matches, returnedBytes, isTruncated = util.AppendSearchResult(matches, returnedBytes, path)
		if isTruncated {
			return fs.SkipAll
		}

		return nil
	})
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	output := util.ReportSearchResults(matches, isTruncated)
	metrics := tool.GetMetrics(output)
	metrics.IsTruncated = isTruncated

	return output, metrics, nil
}
