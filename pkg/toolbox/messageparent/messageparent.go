package messageparent

import (
	"context"
	"errors"
	"strings"

	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/tool"
)

const (
	Name   = "message_parent"
	Result = "sent to your parent"
)

type Args struct {
	Message string `json:"message"`
}

type Parent func(ctx context.Context, message string) error

func New(parent Parent) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name: Name,
			Description: "send your parent a message now and keep working, such as a question, a blocker, or an early finding it can act on; " +
				"your final answer reaches your parent on its own when you finish, so never send it here; " +
				"if your parent replies, its reply reaches you after a later round of tool calls",
			Schema: tool.Schema{
				tool.String("message", "the message, as plain text"),
			},
		},
		Describe,
	).
		Validate(validate).
		Plain(func(ctx context.Context, args Args) (string, error) {
			if err := parent(ctx, strings.TrimSpace(args.Message)); err != nil {
				return "", err
			}
			return Result, nil
		})
}

func Describe(args Args) tool.CallRendering {
	return tool.CallRendering{Subject: strutil.Flatten(args.Message)}
}

func validate(args Args) error {
	if strings.TrimSpace(args.Message) == "" {
		return errors.New("message is required")
	}
	return nil
}
