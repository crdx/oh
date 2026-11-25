package anthropic

import (
	_ "embed"
	"strings"
)

//go:embed guidance/guidance.md
var guidance string

func Guidance() string {
	return strings.TrimSpace(guidance)
}

func (self *Client) Guidance() string {
	return Guidance()
}
