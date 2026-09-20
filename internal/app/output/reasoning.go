package output

import (
	"fmt"
	"slices"
	"strings"
)

type ReasoningRendering int

const (
	ReasoningPlain ReasoningRendering = iota

	ReasoningMarkdown
)

var reasoningRenderings = map[string]ReasoningRendering{
	"markdown": ReasoningMarkdown,
	"plain":    ReasoningPlain,
}

func (self *ReasoningRendering) UnmarshalTOML(value any) error {
	name, isText := value.(string)
	if !isText {
		return fmt.Errorf("reasoning is not one of %s", strings.Join(reasoningRenderingNames(), ", "))
	}

	rendering, isKnown := reasoningRenderings[strings.TrimSpace(name)]
	if !isKnown {
		return fmt.Errorf(
			"reasoning must be one of %s (got %q)",
			strings.Join(reasoningRenderingNames(), ", "), name,
		)
	}

	*self = rendering

	return nil
}

func reasoningRenderingNames() []string {
	names := make([]string, 0, len(reasoningRenderings))
	for name := range reasoningRenderings {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}
