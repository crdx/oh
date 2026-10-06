package jobrecord

import (
	"encoding/json"
	"strings"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
)

const Listing agent.Kind = "job_listing"

func ListingEvent(listing []jobs.Snapshot) agent.Event {
	encodedListing, err := json.Marshal(listing)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: Listing, State: encodedListing}
}

func decodeListing(event agent.Event) ([]jobs.Snapshot, error) {
	var listing []jobs.Snapshot
	if err := json.Unmarshal(event.State, &listing); err != nil {
		return nil, err
	}

	return listing, nil
}

func LastRecorded(events []agent.Event) ([]jobs.Snapshot, bool) {
	return access.LastRecorded(events, Listing, decodeListing)
}

const Ended agent.Kind = "job_ended"

func EndedEvent(conclusion jobs.Conclusion) agent.Event {
	encodedConclusion, err := json.Marshal(conclusion)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: Ended, Name: conclusion.Snapshot.Name, State: encodedConclusion}
}

func EndedNotice(event agent.Event) (string, bool) {
	var conclusion jobs.Conclusion
	if err := json.Unmarshal(event.State, &conclusion); err != nil || conclusion.Snapshot.Name == "" {
		return "", false
	}

	return jobs.Report(
		"Job "+markdown.CodeSpan(conclusion.Snapshot.Name)+" exited: "+conclusion.Snapshot.OutcomeWith(conclusion.Output)+".",
		conclusion.Output,
		conclusion.DroppedBytes,
	), true
}

const EndedWithSession agent.Kind = "jobs_ended_with_session"

func EndedWithSessionEvent(names []string) agent.Event {
	encodedNames, err := json.Marshal(names)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: EndedWithSession, State: encodedNames}
}

func EndedWithSourceSessionEvent(sourceName string, names []string) agent.Event {
	event := EndedWithSessionEvent(names)
	event.Name = sourceName

	return event
}

func EndedWithSessionNotice(event agent.Event) (string, bool) {
	return endedWithSessionNotice(event, "")
}

func EndedWithSessionModelNotice(event agent.Event) (string, bool) {
	return endedWithSessionNotice(event, " with `job(action=\"start\", name=…)`")
}

func endedWithSessionNotice(event agent.Event, instruction string) (string, bool) {
	var names []string
	if err := json.Unmarshal(event.State, &names); err != nil || len(names) == 0 {
		return "", false
	}

	formattedNames := make([]string, 0, len(names))
	for _, name := range names {
		formattedNames = append(formattedNames, markdown.CodeSpan(name))
	}

	subject := "Job " + formattedNames[0]
	pronoun := "it"
	wasRunning := " was running"
	isNotRunning := " is not running"

	if len(formattedNames) > 1 {
		subject = "Jobs " + strings.Join(formattedNames[:len(formattedNames)-1], ", ") +
			" and " + formattedNames[len(formattedNames)-1]
		pronoun = "each"
		wasRunning = " were running"
		isNotRunning = " are not running"
	}

	restart := "Restart " + pronoun + instruction + " if still needed."
	if event.Name != "" {
		return subject + wasRunning + " in session " + markdown.CodeSpan(event.Name) +
			" when this session was forked from it, and" + isNotRunning + " here. " + restart, true
	}

	return "The session was closed and reopened after the last turn. " +
		subject + " stopped when it closed. " + restart, true
}
