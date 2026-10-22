package migrate

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"
)

const (
	historicalJobListing           = "job_listing"
	historicalJobEnded             = "job_ended"
	historicalJobsEndedWithSession = "jobs_ended_with_session"
	historicalJobStop              = "job_stop"
	historicalPortGrantChange      = "port_grant_change"
	taskRunEndedKind               = "task_run_ended"
	tasksEndedWithSessionKind      = "tasks_ended_with_session"
	taskStopKind                   = "task_stop"
	jobToolName                    = "job"
)

type historicalJobSnapshot struct {
	Name      string    `json:"name"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitzero"`
	ExitCode  int       `json:"code,omitempty"`
	Failure   string    `json:"failure,omitempty"`
	Run       int       `json:"run,omitempty"`
}

type historicalJobConclusion struct {
	Snapshot historicalJobSnapshot `json:"snapshot"`
	Output   string                `json:"output,omitempty"`
}

type migratedTaskSnapshot struct {
	ID           string    `json:"id"`
	ToolName     string    `json:"tool_name"`
	Run          int       `json:"run"`
	IsRespawning bool      `json:"is_respawning"`
	State        string    `json:"state"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	Output       string    `json:"output"`
	Failure      string    `json:"failure"`
}

type migratedTaskCompletion struct {
	Snapshot migratedTaskSnapshot `json:"snapshot"`
	IsFinal  bool                 `json:"is_final"`
}

var jobStatesAsTaskStates = map[string]string{
	"complete":               "done",
	"failed":                 "failed",
	"stopped":                "stopped",
	"ended with the session": "stopped",
}

func jobFactsBecomeTaskFacts(lines []map[string]json.RawMessage) ([]map[string]json.RawMessage, error) {
	migratedLines := make([]map[string]json.RawMessage, 0, len(lines))
	for index, line := range lines {
		isKept := true
		err := with(line, func(event map[string]json.RawMessage) error {
			kind, _ := historicalString(event["kind"])
			switch kind {
			case historicalJobListing:
				isKept = false
			case historicalJobEnded:
				return jobEndedBecomesTaskRunEnded(event)
			case historicalJobsEndedWithSession:
				setHistoricalString(event, "kind", tasksEndedWithSessionKind)
			case historicalJobStop:
				setHistoricalString(event, "kind", taskStopKind)
			case historicalPortGrantChange:
				return jobPortsBecomeTaskPorts(event)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", index+1, err)
		}
		if isKept {
			migratedLines = append(migratedLines, line)
		}
	}
	return migratedLines, nil
}

func jobEndedBecomesTaskRunEnded(event map[string]json.RawMessage) error {
	var conclusion historicalJobConclusion
	if err := json.Unmarshal(event["state"], &conclusion); err != nil {
		return fmt.Errorf("the job conclusion could not be read: %w", err)
	}
	snapshot := conclusion.Snapshot
	state, isKnown := jobStatesAsTaskStates[snapshot.State]
	if !isKnown {
		state = "stopped"
	}
	failure := snapshot.Failure
	if failure == "" && snapshot.ExitCode != 0 {
		failure = "exit status " + strconv.Itoa(snapshot.ExitCode)
	}
	completion := migratedTaskCompletion{
		Snapshot: migratedTaskSnapshot{
			ID:        snapshot.Name,
			ToolName:  jobToolName,
			Run:       max(snapshot.Run, 1),
			State:     state,
			StartedAt: snapshot.StartedAt,
			EndedAt:   snapshot.EndedAt,
			Output:    conclusion.Output,
			Failure:   failure,
		},
		IsFinal: true,
	}
	encodedCompletion, err := json.Marshal(completion)
	if err != nil {
		return err
	}
	setHistoricalString(event, "kind", taskRunEndedKind)
	setHistoricalString(event, "name", snapshot.Name)
	event["state"] = encodedCompletion
	return nil
}

func jobPortsBecomeTaskPorts(event map[string]json.RawMessage) error {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(event["state"], &state); err != nil {
		return fmt.Errorf("the port grant could not be read: %w", err)
	}
	rawJobNames, hasJobNames := state["job_names"]
	if !hasJobNames {
		return nil
	}
	var jobNames map[string]string
	if err := json.Unmarshal(rawJobNames, &jobNames); err != nil {
		return fmt.Errorf("the port grant's job names could not be read: %w", err)
	}
	taskNames := make(map[string]string)
	if rawTaskNames, hasTaskNames := state["task_names"]; hasTaskNames {
		if err := json.Unmarshal(rawTaskNames, &taskNames); err != nil {
			return fmt.Errorf("the port grant's task names could not be read: %w", err)
		}
	}
	for _, port := range slices.Sorted(maps.Keys(jobNames)) {
		if _, isTaken := taskNames[port]; !isTaken {
			taskNames[port] = jobNames[port]
		}
	}
	delete(state, "job_names")
	if len(taskNames) > 0 {
		encodedTaskNames, err := json.Marshal(taskNames)
		if err != nil {
			return err
		}
		state["task_names"] = encodedTaskNames
	}
	encodedState, err := json.Marshal(state)
	if err != nil {
		return err
	}
	event["state"] = encodedState
	return nil
}
