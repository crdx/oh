package sequence

import "crdx.org/oh/internal/mermaid/diagram"

func upstreamTestConfig(shouldUseASCII bool) *diagram.Config {
	config := diagram.DefaultConfig()
	config.UseAscii = shouldUseASCII
	return config
}
