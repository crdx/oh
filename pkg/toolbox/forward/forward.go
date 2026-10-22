package forward

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

const Name = "forward"

const (
	actionAdd     = "add"
	actionRemove  = "remove"
	actionList    = "list"
	actionChoices = "add, remove, or list"
)

var actions = []string{actionAdd, actionRemove, actionList}

type Publication struct {
	Port     uint16
	TaskName string
	URL      string
}

type Ports interface {
	Forward(port uint16, taskName string) (string, error)
	Revoke(port uint16) error
	List() []Publication
}

type Args struct {
	Action   string `json:"action"`
	Port     int    `json:"port,omitempty"`
	TaskName string `json:"task_name,omitempty"`
}

func New(ports Ports) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name: Name,
			Description: "forward a port of this sandbox to the user's machine, " +
				"so a server started in the background can be opened in their browser",
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.Integer("port", "the TCP port inside the sandbox (for add and remove)").Optional(),
				tool.String("task_name", "an optional associated task name or id matching "+tasks.IDPattern()+" (for add)").Optional(),
			},
		},
		Describe,
	).
		Validate(validate).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			if ctx.Err() != nil {
				return "", tool.ToolCallMetrics{}, stop.Error(ctx, "the port request")
			}
			output, err := run(ports, args)
			if err != nil {
				return output, tool.ToolCallMetrics{}, stop.ErrorFrom(ctx, "the port request", err)
			}
			return output, tool.ToolCallMetrics{}, nil
		})
}

func Describe(args Args) tool.CallRendering {
	port := strconv.Itoa(args.Port)
	switch args.Action {
	case actionAdd:
		if args.TaskName != "" {
			port = args.TaskName + ":" + port
		}
		return tool.CallRendering{Kind: "forward_add", Subject: port, ReportsStatus: true}
	case actionRemove:
		return tool.CallRendering{Kind: "forward_remove", Subject: port, ReportsStatus: true}
	case actionList:
		return tool.CallRendering{Kind: "forward_list", Subject: "forwards"}
	}

	return tool.CallRendering{}
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action must be %s (got %q)", actionChoices, args.Action)
	}
	if args.Action == actionList {
		if args.Port != 0 {
			return errors.New("list does not accept port")
		}
		if args.TaskName != "" {
			return errors.New("list does not accept task_name")
		}

		return nil
	}
	if args.Port < 1 || args.Port > 65535 {
		return fmt.Errorf("port must be 1–65535 (got %d)", args.Port)
	}
	if args.Action == actionRemove && args.TaskName != "" {
		return errors.New("remove does not accept task_name")
	}
	if args.TaskName != "" {
		return tasks.ValidateID(args.TaskName)
	}

	return nil
}

func run(ports Ports, args Args) (string, error) {
	port, err := portOf(args)
	if err != nil {
		return "", err
	}

	switch args.Action {
	case actionAdd:
		return Publish(ports, port, args.TaskName)
	case actionRemove:
		if err := ports.Revoke(port); err != nil {
			return "", err
		}

		return "Forward removed.", nil
	case actionList:
		return list(ports), nil
	}

	return "", fmt.Errorf("unknown action %q", args.Action)
}

func Publish(ports Ports, port uint16, taskName string) (string, error) {
	address, err := ports.Forward(port, taskName)
	if err != nil {
		return "", err
	}

	return publication(address, port), nil
}

func publication(address string, port uint16) string {
	return "Use " + address + " for the user; use localhost:" + strconv.Itoa(int(port)) + " inside the sandbox."
}

func portOf(args Args) (uint16, error) {
	if args.Action == actionList {
		return 0, nil
	}
	if args.Port < 1 || args.Port > 65535 {
		return 0, fmt.Errorf("port must be 1–65535 (got %d)", args.Port)
	}

	return uint16(args.Port), nil
}

func list(ports Ports) string {
	publications := ports.List()
	if len(publications) == 0 {
		return "No ports are forwarded."
	}

	lines := make([]string, 0, len(publications))
	for _, publication := range publications {
		name := strconv.Itoa(int(publication.Port))
		if publication.TaskName != "" {
			name = publication.TaskName + ":" + name
		}
		lines = append(lines, name+"  "+publication.URL)
	}

	return strings.Join(lines, "\n")
}
