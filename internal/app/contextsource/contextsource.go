package contextsource

import "crdx.org/oh/internal/util"

type Kind string

const (
	HarnessInstructions Kind = "harness_instructions"
	SkillDefinitions    Kind = "skill_definitions"
	ToolDefinitions     Kind = "tool_definitions"
)

type Source struct {
	Kind            Kind   `json:"kind,omitempty"`
	Count           int    `json:"count,omitempty"`
	Path            string `json:"path,omitempty"`
	EstimatedTokens int64  `json:"estimated_tokens,omitempty"`
}

func KindFromBytes(kind Kind, bytes int) Source {
	return CountedFromBytes(kind, 0, bytes)
}

func CountedFromBytes(kind Kind, count int, bytes int) Source {
	return Source{
		Kind:            kind,
		Count:           count,
		EstimatedTokens: util.EstimateTokenCount(bytes),
	}
}

func FileFromBytes(path string, bytes int) Source {
	return Source{
		Path:            path,
		EstimatedTokens: util.EstimateTokenCount(bytes),
	}
}

func (self Source) DisplayName() string {
	if self.Path != "" {
		return self.Path
	}

	switch self.Kind {
	case HarnessInstructions:
		return "harness instructions"
	case SkillDefinitions:
		return util.Plural(self.Count, "skill definition")
	case ToolDefinitions:
		return util.Plural(self.Count, "tool definition")
	}

	return string(self.Kind)
}
