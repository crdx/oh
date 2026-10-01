package call

import (
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type ToolLookup func(string) (tool.Tool, bool)

func Summary(event agent.Event, shouldShowOutput bool) string {
	if event.Status != agent.SuccessStatus || shouldShowOutput {
		return event.Text
	}

	return ""
}

func Describe(event agent.Event, getTool ToolLookup, workspace *work.Space) agent.FallbackRendering {
	rendering, _ := describe(event, getTool, workspace)
	return rendering
}

func describe(
	event agent.Event,
	getTool ToolLookup,
	workspace *work.Space,
) (agent.FallbackRendering, time.Duration) {
	rendering := event.FallbackRendering
	calledTool, isKnown := findTool(getTool, event.Name)
	if !isKnown {
		return plain(shortenPaths(rendering, workspace)), 0
	}

	rendering, timeLimit := describeKnownTool(event, rendering, calledTool)
	return plain(shortenPaths(rendering, workspace)), timeLimit
}

func findTool(getTool ToolLookup, name string) (tool.Tool, bool) {
	if getTool == nil {
		return nil, false
	}
	return getTool(name)
}

func describeKnownTool(
	event agent.Event,
	rendering agent.FallbackRendering,
	calledTool tool.Tool,
) (agent.FallbackRendering, time.Duration) {
	rendering.ReadOnly = calledTool.ReadOnly()
	rendering.MarksSuccess = tool.MarksSuccess(calledTool)
	parsedToolCall, err := calledTool.Parse(event.Arguments)
	if err == nil {
		rendering.Describe(parsedToolCall)
		return rendering, parsedToolCall.TimeLimit()
	}

	fallback, argumentsWereDecoded := calledTool.Render(event.Arguments)
	rendering.SetRendering(fallback)
	if !argumentsWereDecoded || !fallback.HasArguments() {
		rendering.Subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
	}
	return rendering, 0
}

func plain(rendering agent.FallbackRendering) agent.FallbackRendering {
	primary := printableCallRendering(tool.CallRendering{
		Subject:   rendering.Subject,
		Qualifier: rendering.Note,
		Emphasis:  rendering.Emphasis,
	})
	rendering.Subject = primary.Subject
	rendering.Note = primary.Qualifier
	rendering.Emphasis = primary.Emphasis
	rendering.Continuation = slices.Clone(rendering.Continuation)
	for i := range rendering.Continuation {
		rendering.Continuation[i] = printableCallRendering(rendering.Continuation[i])
	}

	return rendering
}

func printableCallRendering(rendering tool.CallRendering) tool.CallRendering {
	rendering.Subject = strings.TrimSpace(strutil.Printable(rendering.Subject))
	rendering.Qualifier = strings.TrimSpace(strutil.Printable(rendering.Qualifier))
	return rendering
}

func LabelFor(event agent.Event, getTool ToolLookup, workspace *work.Space) Label {
	rendering, timeLimit := describe(event, getTool, workspace)
	renderingKind := rendering.RenderingKind
	if renderingKind == "" {
		renderingKind = event.Name
	}
	label := getLabel(tool.CallRendering{
		Kind:       renderingKind,
		Subject:    rendering.Subject,
		Qualifier:  rendering.Note,
		PathLine:   rendering.PathLine,
		Emphasis:   rendering.Emphasis,
		ShowOutput: rendering.ShowOutput,
	}, event.Name, rendering.ReadOnly)
	label.TimeLimit = timeLimit
	label.MarksSuccess = rendering.MarksSuccess
	label.lineRange = rendering.PathLine
	label.Continuation = make([]Label, 0, len(rendering.Continuation))
	for _, part := range rendering.Continuation {
		label.Continuation = append(label.Continuation, getLabel(part, part.Kind, false))
	}

	return label
}

func LabelForRendering(rendering tool.CallRendering) Label {
	label := getLabel(rendering, rendering.Kind, false)
	label.lineRange = rendering.PathLine
	return label
}

func getLabel(rendering tool.CallRendering, fallbackName string, isReadOnly bool) Label {
	name, nameStyle, focusStyle := style.ToolCallAppearance(rendering.Kind, fallbackName, isReadOnly)
	return Label{
		Name:       name,
		Subject:    rendering.Subject,
		Emphasis:   rendering.Emphasis,
		Qualifier:  rendering.Qualifier,
		ReadOnly:   isReadOnly,
		NameStyle:  nameStyle,
		FocusStyle: focusStyle,
		ShowOutput: rendering.ShowOutput,
	}
}

func ToolMark(kind string) (string, style.Style) {
	name, nameStyle, _ := style.ToolCallAppearance(kind, "", false)
	return name, nameStyle
}
