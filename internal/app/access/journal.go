package access

import (
	"slices"

	"crdx.org/oh/pkg/agent"
)

func LastRecorded[Value any](
	events []agent.Event,
	kind agent.Kind,
	decode func(agent.Event) (Value, error),
) (Value, bool) {
	for _, event := range slices.Backward(events) {
		if event.Kind != kind {
			continue
		}

		value, err := decode(event)
		if err == nil {
			return value, true
		}
	}

	var zero Value
	return zero, false
}
