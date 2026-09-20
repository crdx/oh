package mermaid

import (
	"strings"

	"crdx.org/oh/internal/mermaid/diagram"
)

func upstreamASCIIFlowchartConfig() *diagram.Config {
	config := diagram.DefaultConfig()
	config.UseAscii = true
	return config
}

func renderUpstreamDiagram(source string, config *diagram.Config) (string, error) {
	source, title := diagram.StripFrontmatter(source)
	renderer := rendererFor(source)
	if err := renderer.Parse(source); err != nil {
		return "", err
	}
	output, err := renderer.Render(config)
	if err != nil {
		return "", err
	}
	if title != "" {
		output = title + "\n\n" + output
	}
	return strings.TrimRight(output, "\n"), nil
}
