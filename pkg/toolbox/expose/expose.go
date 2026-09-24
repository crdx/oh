package expose

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/tool"
)

const (
	actionAdd     = "add"
	actionRemove  = "remove"
	actionList    = "list"
	actionChoices = "add, remove, or list"
)

var actions = []string{actionAdd, actionRemove, actionList}

type Publication struct {
	Port    uint16
	JobName string
	URL     string
}

type Ports interface {
	Expose(port uint16, jobName string) (string, error)
	Hide(port uint16) error
	List() []Publication
}

type Args struct {
	Action  string `json:"action"`
	Port    int    `json:"port,omitempty"`
	JobName string `json:"job_name,omitempty"`
}

func New(ports Ports) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name: "expose",
			Description: "make a port of this sandbox reachable from the user's machine, " +
				"so a server started with the job tool can be opened in their browser",
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.Integer("port", "the TCP port inside the sandbox (for add and remove)").Optional(),
				tool.String("job_name", fmt.Sprintf("an optional associated job name; 1–%d characters from [a-z0-9-] (for add)", jobs.NameLengthLimit)).Optional(),
			},
		},
		Describe,
	).
		Validate(validate).
		Exec(func(_ context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return run(ports, args)
		})
}

func Describe(args Args) (string, string) {
	if args.Action == actionList {
		return "", args.Action
	}
	if args.Action == actionAdd {
		return strconv.Itoa(args.Port), ""
	}

	return strconv.Itoa(args.Port), args.Action
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action must be %s (got %q)", actionChoices, args.Action)
	}
	if args.Action == actionList {
		if args.Port != 0 {
			return errors.New("list does not accept port")
		}
		if args.JobName != "" {
			return errors.New("list does not accept job_name")
		}

		return nil
	}
	if args.Port < 1 || args.Port > 65535 {
		return fmt.Errorf("port must be 1–65535 (got %d)", args.Port)
	}
	if args.Action == actionRemove && args.JobName != "" {
		return errors.New("remove does not accept job_name")
	}
	if args.JobName != "" {
		return jobs.ValidateName(args.JobName)
	}

	return nil
}

func run(ports Ports, args Args) (string, tool.ToolCallMetrics, error) {
	port, err := portOf(args)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	switch args.Action {
	case actionAdd:
		address, err := ports.Expose(port, args.JobName)
		if err != nil {
			return "", tool.ToolCallMetrics{}, err
		}

		return "Use " + address + " for the user; use localhost:" + strconv.Itoa(args.Port) +
			" inside the sandbox.", tool.ToolCallMetrics{}, nil
	case actionRemove:
		if err := ports.Hide(port); err != nil {
			return "", tool.ToolCallMetrics{}, err
		}

		return "Exposure removed.", tool.ToolCallMetrics{}, nil
	case actionList:
		return list(ports), tool.ToolCallMetrics{}, nil
	}

	return "", tool.ToolCallMetrics{}, fmt.Errorf("unknown action %q", args.Action)
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
		return "No ports are exposed."
	}

	lines := make([]string, 0, len(publications))
	for _, publication := range publications {
		name := strconv.Itoa(int(publication.Port))
		if publication.JobName != "" {
			name = publication.JobName + ":" + name
		}
		lines = append(lines, name+"  "+publication.URL)
	}

	return strings.Join(lines, "\n")
}
