package taskrecord

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
)

const (
	RunEnded         agent.Kind = "task_run_ended"
	Finished         agent.Kind = "task_run_finished"
	Listing          agent.Kind = "task_listing"
	EndedWithSession agent.Kind = "tasks_ended_with_session"
)

type Completion struct {
	Snapshot       tasks.Snapshot `json:"snapshot"`
	IsFinal        bool           `json:"is_final"`
	IsSilent       bool           `json:"silent,omitempty"`
	IsSessionEnded bool           `json:"ended_with_session,omitempty"`
	IsRespawnSpent bool           `json:"respawn_spent,omitempty"`
}

func EndedEvent(conclusion tasks.Conclusion) agent.Event {
	fact := Completion{
		Snapshot:       conclusion.Snapshot,
		IsFinal:        conclusion.IsFinal,
		IsSilent:       conclusion.IsSilent,
		IsSessionEnded: conclusion.IsSessionEnded,
		IsRespawnSpent: conclusion.IsRespawnSpent,
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: RunEnded, Name: conclusion.Snapshot.ID, State: payload}
}

func FinishedEvent(conclusion tasks.Conclusion) agent.Event {
	event := EndedEvent(conclusion)
	event.Kind = Finished
	return event
}

type runReference struct {
	name string
	run  int
}

func PendingNotices(events []agent.Event) []agent.Event {
	pendingCompletions := make(map[runReference]Completion)
	var order []runReference
	for _, event := range events {
		if event.Kind == Finished || event.Kind == RunEnded {
			var completion Completion
			if json.Unmarshal(event.State, &completion) != nil || completion.Snapshot.ID == "" {
				continue
			}
			reference := runReference{name: completion.Snapshot.ID, run: completion.Snapshot.Run}
			if event.Kind == RunEnded {
				delete(pendingCompletions, reference)
				continue
			}
			if completion.IsSilent && !completion.IsSessionEnded {
				continue
			}
			if _, isPending := pendingCompletions[reference]; !isPending {
				order = append(order, reference)
			}
			pendingCompletions[reference] = completion
			continue
		}
		var names []string
		if event.Kind == EndedWithSession && json.Unmarshal(event.State, &names) == nil {
			for reference, completion := range pendingCompletions {
				if completion.IsSessionEnded && slices.Contains(names, reference.name) {
					delete(pendingCompletions, reference)
				}
			}
		}
	}
	var notices []agent.Event
	var stoppedNames []string
	for _, reference := range order {
		completion, isPending := pendingCompletions[reference]
		if !isPending {
			continue
		}
		delete(pendingCompletions, reference)
		if completion.IsSessionEnded {
			if !slices.Contains(stoppedNames, reference.name) {
				stoppedNames = append(stoppedNames, reference.name)
			}
			continue
		}
		payload, err := json.Marshal(completion)
		if err == nil {
			notices = append(notices, agent.Event{Kind: RunEnded, Name: reference.name, State: payload})
		}
	}
	if len(stoppedNames) > 0 {
		notices = append(notices, EndedWithSessionEvent(stoppedNames))
	}
	return notices
}

func EndedNotice(event agent.Event) (string, bool) {
	var fact Completion
	if event.Kind != RunEnded || json.Unmarshal(event.State, &fact) != nil || fact.Snapshot.ID == "" {
		return "", false
	}
	snapshot := fact.Snapshot
	status := "Task " + markdown.CodeSpan(snapshot.ID)
	if snapshot.Run > 1 {
		status += fmt.Sprintf(" run %d", snapshot.Run)
	}
	status += " " + string(snapshot.State)
	if !fact.IsFinal {
		status += "; respawning"
	}
	if fact.IsRespawnSpent {
		status += fmt.Sprintf("; respawning stopped after %d runs in a row each ended within %s", tasks.QuickRunsTolerated, tasks.QuickRunLimit)
	}
	if snapshot.Failure != "" {
		status += ": " + snapshot.Failure
	}
	status += "."
	if output := strings.TrimRight(snapshot.Output, "\n"); output != "" {
		status += "\n\n" + markdown.CodeBlock("", output)
	}
	return status, true
}

func ListingEvent(snapshots []tasks.Snapshot) agent.Event {
	payload, err := json.Marshal(snapshots)
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: Listing, State: payload}
}

func LastRecorded(events []agent.Event) ([]tasks.Snapshot, bool) {
	return access.LastRecorded(events, Listing, func(event agent.Event) ([]tasks.Snapshot, error) {
		var snapshots []tasks.Snapshot
		err := json.Unmarshal(event.State, &snapshots)
		return snapshots, err
	})
}

func EndedWithSessionEvent(names []string) agent.Event {
	payload, err := json.Marshal(names)
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: EndedWithSession, State: payload}
}

func EndedWithSessionNotice(event agent.Event) (string, bool) {
	var names []string
	if event.Kind != EndedWithSession || json.Unmarshal(event.State, &names) != nil || len(names) == 0 {
		return "", false
	}
	subject, absence, stop := "Task", "It is not running here; task restart starts it again.",
		"It stopped and does not restart automatically; task restart starts it again."
	if len(names) > 1 {
		subject, absence, stop = "Tasks", "They are not running here; task restart starts one again.",
			"They stopped and do not restart automatically; task restart starts one again."
	}
	if event.Name != "" {
		return fmt.Sprintf("%s %s %s running in session %s when this session was forked from it. %s",
			subject, strings.Join(names, ", "), pluralWas(len(names)), markdown.CodeSpan(event.Name), absence), true
	}
	return fmt.Sprintf("The session closed while %s %s running. %s", strings.Join(names, ", "), pluralWas(len(names)), stop), true
}

func pluralWas(count int) string {
	if count == 1 {
		return "was"
	}
	return "were"
}
