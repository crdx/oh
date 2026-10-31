package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/vt"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/fetch"
	"crdx.org/oh/pkg/toolbox/lookup"
)

const renderLines = 24

func RenderJournal(events []agent.Event, columns int) (string, error) {
	defer style.ForceColour()()

	final, err := settledReplay(events, columns)
	if err != nil {
		return "", err
	}

	owners, err := rowOwners(events, columns, final)
	if err != nil {
		return "", err
	}

	return vt.WithRegions(final.Styled(), owners, func(owner int) string {
		return RegionName(events[owner-1])
	}), nil
}

func rowOwners(events []agent.Event, columns int, final *vt.Screen) ([]int, error) {
	finalRows := final.Text()
	owners := make([]int, len(finalRows))
	settledFrom := make([]int, len(finalRows))
	for row := range settledFrom {
		settledFrom[row] = len(events)
	}

	for count := len(events) - 1; count >= 1; count-- {
		earlier, err := settledReplay(events[:count], columns)
		if err != nil {
			return nil, err
		}
		earlierRows := earlier.Text()
		for row := range finalRows {
			if settledFrom[row] == count+1 && row < len(earlierRows) && earlierRows[row] == finalRows[row] {
				settledFrom[row] = count
			}
		}
	}

	for row, text := range finalRows {
		if strings.TrimSpace(text) == "" {
			continue
		}
		owners[row] = settledFrom[row]
	}

	return owners, nil
}

func RegionName(event agent.Event) string {
	switch event.Kind {
	case agent.StartupEvent:
		return "banner"
	case agent.UserMessageEvent:
		return "user"
	case agent.ModelMessageEvent:
		return "answer"
	case agent.ModelReasoningEvent:
		return "reasoning"
	case agent.ToolCallRequestEvent, agent.ToolCallResultEvent:
		return "tool:" + event.Name
	case agent.FailureEvent:
		return "failure"
	case agent.InterruptionEvent:
		return "interruption"
	case agent.RetryingEvent:
		return "retry"
	case agent.SilentTurnEvent, agent.PrefixRewriteEvent, agent.CacheRebuildEvent, agent.StateChangeEvent:
		return string(event.Kind)
	default:
		return string(event.Kind)
	}
}

func settledReplay(events []agent.Event, columns int) (*vt.Screen, error) {
	stream, err := replayStream(events, columns)
	if err != nil {
		return nil, err
	}

	screen := vt.New(columns, 0)
	if err := screen.Play(stream); err != nil {
		return nil, fmt.Errorf("could not settle the drawing: %w", err)
	}

	return screen, nil
}

func replayStream(events []agent.Event, columns int) (string, error) {
	workspaceDir, err := os.MkdirTemp("", "oh-render-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(workspaceDir) }()

	workspace := work.At(workspaceDir)
	if err := workspace.Open(); err != nil {
		return "", err
	}
	defer func() { _ = workspace.Close() }()

	var stream strings.Builder
	chat := &App{
		agent:     agent.New("", renderProvider{}, renderTools(workspace)),
		screen:    output.NewTerminalOfSize(&stream, columns, renderLines),
		workspace: workspace,
	}
	chat.recordedEvents = events
	chat.replay()

	return stream.String(), nil
}

func renderTools(workspace *work.Space) []tool.Tool {
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(caps.NewMode(caps.All())))

	tools := toolbox.Rummage(files, file.NewSnapshots())

	return append(
		tools,
		bash.New(
			files,
			func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
			func(context.Context, string, string) error { return nil },
			sandbox.Direct(),
			true,
		),
		lookup.New(func() bool { return true }, refuseRenderCall, nil),
		fetch.New(func() bool { return true }, refuseRenderCall, nil),
	)
}

var errRenderRunsNoTools = errors.New("a rendering runs no tools")

func refuseRenderCall(context.Context, string) error {
	return errRenderRunsNoTools
}

type renderProvider struct{}

func (renderProvider) Configure(string, []tool.Definition)   {}
func (renderProvider) AddUserMessage(string)                 {}
func (renderProvider) AddToolResults([]agent.ToolCallResult) {}
func (renderProvider) Dump() []json.RawMessage               { return nil }
func (renderProvider) Load([]json.RawMessage)                {}

func (renderProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}
