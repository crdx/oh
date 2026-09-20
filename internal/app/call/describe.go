package call

import (
	"slices"
	"strings"
	"time"

	"crdx.org/io/internal/app/skill"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
)

const (
	readTool   = "read"
	shellTool  = "bash"
	lookupTool = "lookup"
	fetchTool  = "fetch"
)

type ToolLookup func(string) (tool.Tool, bool)

func Summary(event agent.Event) string {
	if event.Status != agent.SuccessStatus || event.Name == shellTool {
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
	var timeLimit time.Duration

	if getTool != nil {
		if calledTool, isKnown := getTool(event.Name); isKnown {
			rendering.ReadOnly = calledTool.ReadOnly()
			if parsedToolCall, err := calledTool.Parse(event.Arguments); err == nil {
				rendering.Describe(parsedToolCall)
				timeLimit = parsedToolCall.TimeLimit()
			} else {
				rendering.Subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
			}
		}
	}

	return plain(shortenPaths(rendering, workspace)), timeLimit
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
	label := getLabel(event.Name, rendering.Subject, rendering.Note, rendering.Emphasis, rendering.ReadOnly)
	label.TimeLimit = timeLimit
	label.Continuation = make([]Label, 0, len(rendering.Continuation))
	for _, part := range rendering.Continuation {
		label.Continuation = append(label.Continuation, getLabel(part.Name, part.Subject, part.Qualifier, part.Emphasis, false))
	}

	skillName, isSkillLoad := "", false
	if event.Name == readTool {
		label.lineRange = rendering.Note
		skillName, isSkillLoad = skill.NameFromPath(rendering.Subject)
	}

	if isSkillLoad {
		label.Name = "load"
		label.NameStyle = style.Skill
		label.Accent = skillName
		label.AccentStyle = style.Skill
		label.Emphasis = tool.Emphasis{}
	}
	return label
}

func LabelForRendering(rendering tool.CallRendering) Label {
	return getLabel(
		rendering.Name,
		rendering.Subject,
		rendering.Qualifier,
		rendering.Emphasis,
		false,
	)
}

func getLabel(name string, subject string, qualifier string, emphasis tool.Emphasis, isReadOnly bool) Label {
	label := Label{
		Name:      name,
		Subject:   subject,
		Emphasis:  emphasis,
		Qualifier: qualifier,
		ReadOnly:  isReadOnly,
	}

	if toolLabel, isKnown := toolLabels[name]; isKnown {
		label.Name = toolLabel.name
		label.NameStyle = toolLabel.style
	}

	return label
}

func ToolMark(name string) (string, style.Style) {
	if toolLabel, isKnown := toolLabels[name]; isKnown {
		return toolLabel.name, toolLabel.style
	}

	return "", nil
}

var toolLabels = map[string]struct {
	name  string
	style style.Style
}{
	shellTool:  {"$", style.Shell},
	lookupTool: {"lookup", style.Lookup},
	fetchTool:  {"fetch", style.Network},
}
