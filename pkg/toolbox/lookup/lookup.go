package lookup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/tool"
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
		Describe,
	).
		DefaultsTo(Describe(Args{})).
		Validate(validate).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Requires(isAllowed, ErrWithheld).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			if ctx.Err() != nil {
				return "", tool.ToolCallMetrics{}, stop.Error(ctx, "the search")
			}
			if err := approve(ctx, args.Query); err != nil {
				return "", tool.ToolCallMetrics{}, stop.ErrorFrom(ctx, "the search", err)
			}

			output, err := searcher.Search(ctx, args.Query)
			if err != nil {
				err = stop.ErrorFrom(ctx, "the search", err)
				if output == "" {
					return "", tool.ToolCallMetrics{}, err
				}
				output = tool.MarkUntrusted(fmt.Sprintf("a web search for %q", args.Query), output)
				output = stop.Report(output, err)
				return output, tool.GetMetrics(output), err
			}
			if ctx.Err() != nil {
				failure := stop.Error(ctx, "the search")
				if output == "" {
					return "", tool.ToolCallMetrics{}, failure
				}
				output = tool.MarkUntrusted(fmt.Sprintf("a web search for %q", args.Query), output)
				output = stop.Report(output, failure)
				return output, tool.GetMetrics(output), failure
			}
			if output == "" {
				return "", tool.ToolCallMetrics{}, errors.New("lookup returned no content")
			}

			output = tool.MarkUntrusted(fmt.Sprintf("a web search for %q", args.Query), output)

			return output, tool.GetMetrics(output), nil
		})
}

func Describe(args Args) tool.CallRendering {
	return tool.CallRendering{Kind: "lookup", Subject: args.Query}
}

func validate(args Args) error {
	if strings.TrimSpace(args.Query) == "" {
		return errors.New("query is required")
	}

	return nil
}
