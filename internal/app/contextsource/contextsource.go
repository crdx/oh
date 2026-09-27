package contextsource

import "crdx.org/oh/internal/util"

type Source struct {
	Name            string `json:"name,omitempty"`
	Path            string `json:"path,omitempty"`
	EstimatedTokens int64  `json:"estimated_tokens,omitempty"`
}

func NamedFromBytes(name string, bytes int) Source {
	return Source{
		Name:            name,
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
	return self.Name
}
