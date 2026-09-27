package contextfile

import "crdx.org/oh/internal/util"

type File struct {
	Path            string `json:"path"`
	EstimatedTokens int64  `json:"estimated_tokens,omitempty"`
}

func FromBytes(path string, bytes int) File {
	return File{
		Path:            path,
		EstimatedTokens: util.EstimateTokenCount(bytes),
	}
}
