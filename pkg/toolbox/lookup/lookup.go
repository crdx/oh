package lookup

import (
	"context"
	"errors"
	"strings"

	"crdx.org/io/pkg/tool"
)

var ErrWithheld = errors.New("lookup access unavailable; ctrl+x l grants it")

type Args struct {
	Query string `json:"query"`
}

type Searcher interface {
	Search(context context.Context, query string) (string, error)
}

func New(
	isAllowed func() bool,
	approve func(context.Context, string) error,
	searcher Searcher,
) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "lookup",
			Description: "search the web using OpenAI and return a cited answer",
			Schema: tool.Schema{
				tool.String("query", "web lookup query"),
			},
		},
		func(args Args) (string, string) { return args.Query, "" },
	).
		Validate(validate).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Requires(isAllowed, ErrWithheld).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			if err := approve(ctx, args.Query); err != nil {
				return "", tool.ToolCallMetrics{}, err
			}

			output, err := searcher.Search(ctx, args.Query)
			if err != nil {
				return "", tool.ToolCallMetrics{}, err
			}
			if output == "" {
				return "", tool.ToolCallMetrics{}, errors.New("lookup returned no content")
			}

			return output, tool.GetMetrics(output), nil
		})
}

func validate(args Args) error {
	if strings.TrimSpace(args.Query) == "" {
		return errors.New("query is required")
	}

	return nil
}
