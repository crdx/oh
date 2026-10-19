package call

import (
	"time"

	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
)

type Describer struct {
	getTool      ToolLookup
	workspace    *work.Space
	descriptions map[callKey]callDescription
}

type callKey struct {
	name      string
	arguments string
}

type callDescription struct {
	rendering agent.FallbackRendering
	timeLimit time.Duration
}

func NewDescriber(getTool ToolLookup, workspace *work.Space) *Describer {
	return &Describer{
		getTool:      getTool,
		workspace:    workspace,
		descriptions: map[callKey]callDescription{},
	}
}

func (self *Describer) LabelFor(event agent.Event) Label {
	calledTool, isKnown := findTool(self.getTool, event.Name)
	if !isKnown {
		return LabelFor(event, nil, self.workspace)
	}

	key := callKey{name: event.Name, arguments: event.Arguments}
	description, isDescribed := self.descriptions[key]
	if !isDescribed {
		rendering, timeLimit := describeKnownTool(event, event.FallbackRendering, calledTool)
		description = callDescription{rendering: plain(shortenPaths(rendering, self.workspace)), timeLimit: timeLimit}
		self.descriptions[key] = description
	}

	return labelFrom(event.Name, description.rendering, description.timeLimit)
}
