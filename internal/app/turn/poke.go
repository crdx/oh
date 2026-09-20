package turn

import "crdx.org/io/pkg/agent"

const HarnessPoke agent.Kind = "harness_poke"

const PokeMessage = "No reply was returned; continue from where you stopped."

func PokeEvent() agent.Event {
	return agent.Event{Kind: HarnessPoke}
}

func PokeNotice(event agent.Event) (string, bool) {
	if event.Kind != HarnessPoke {
		return "", false
	}

	return PokeMessage, true
}
