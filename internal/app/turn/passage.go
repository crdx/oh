package turn

import (
	"encoding/json"
	"fmt"
	"time"

	"crdx.org/oh/pkg/agent"
)

const TimePassage agent.Kind = "time_passage"

const passageAdvice = "Anything read, run, or assumed before then may have changed, so check it again before relying on it."

type passageState struct {
	ElapsedSeconds int64 `json:"elapsed_seconds"`
}

func PassageEvent(elapsedTime time.Duration) (agent.Event, bool) {
	state, err := json.Marshal(passageState{ElapsedSeconds: int64(elapsedTime / time.Second)})
	if err != nil {
		return agent.Event{}, false
	}

	return agent.Event{Kind: TimePassage, State: state}, true
}

func PassageNotice(event agent.Event) (string, bool) {
	if event.Kind != TimePassage {
		return "", false
	}

	var state passageState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return "", false
	}

	elapsedTime := time.Duration(state.ElapsedSeconds) * time.Second
	return "About " + roughly(elapsedTime) + " passed since the previous turn ended. " + passageAdvice, true
}

func roughly(elapsedTime time.Duration) string {
	const day = 24 * time.Hour

	switch {
	case elapsedTime < time.Minute:
		return count(int(elapsedTime/time.Second), "second")
	case elapsedTime < time.Hour:
		return count(int(elapsedTime/time.Minute), "minute")
	case elapsedTime < 2*day:
		return count(int(elapsedTime.Round(time.Hour)/time.Hour), "hour")
	}

	return count(int(elapsedTime.Round(day)/day), "day")
}

func count(quantity int, unit string) string {
	if quantity == 1 {
		return "1 " + unit
	}

	return fmt.Sprintf("%d %ss", quantity, unit)
}
