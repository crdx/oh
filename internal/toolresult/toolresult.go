package toolresult

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

const (
	scheme         = "oh"
	toolResultHost = "tool-result"
	taskResultHost = "task-result"
)

var errResultFound = errors.New("tool result found")

type Reference struct {
	SessionName string
	CallID      string
}

type TaskReference struct {
	EndedAt     time.Time
	SessionName string
	TaskName    string
	Run         int
}

type Exchange struct {
	Request agent.Event
	Result  agent.Event
}

func URL(sessionName string, callID string) string {
	target := url.URL{Scheme: scheme, Host: toolResultHost}
	query := url.Values{}
	query.Set("session", sessionName)
	query.Set("call", callID)
	target.RawQuery = query.Encode()
	return target.String()
}

func TaskURL(reference TaskReference) string {
	target := url.URL{Scheme: scheme, Host: taskResultHost}
	query := url.Values{}
	query.Set("session", reference.SessionName)
	query.Set("task", reference.TaskName)
	query.Set("run", strconv.Itoa(reference.Run))
	if !reference.EndedAt.IsZero() {
		query.Set("ended", strconv.FormatInt(reference.EndedAt.UnixNano(), 10))
	}
	target.RawQuery = query.Encode()
	return target.String()
}

func IsTask(address string) bool {
	target, err := url.Parse(address)
	return err == nil && target.Scheme == scheme && target.Host == taskResultHost
}

func ParseTask(address string) (TaskReference, error) {
	keys := []string{"session", "task", "run"}
	if target, err := url.Parse(address); err == nil && target.Query().Has("ended") {
		keys = append(keys, "ended")
	}
	query, err := parseQuery(address, taskResultHost, keys...)
	if err != nil {
		return TaskReference{}, fmt.Errorf("invalid task result link: %w", err)
	}
	run, err := strconv.Atoi(query.Get("run"))
	if err != nil || run < 1 {
		return TaskReference{}, errors.New("invalid task result link: the run is not a positive whole number")
	}
	reference := TaskReference{SessionName: query.Get("session"), TaskName: query.Get("task"), Run: run}
	if query.Has("ended") {
		endedAt, err := strconv.ParseInt(query.Get("ended"), 10, 64)
		if err != nil {
			return TaskReference{}, errors.New("invalid task result link: the end time is not a whole number")
		}
		reference.EndedAt = time.Unix(0, endedAt)
	}
	return reference, nil
}

func parseQuery(address string, host string, keys ...string) (url.Values, error) {
	target, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if target.Scheme != scheme || target.Host != host || (target.Path != "" && target.Path != "/") || target.Fragment != "" || target.User != nil {
		return nil, errors.New("unknown address")
	}
	query := target.Query()
	if len(query) != len(keys) {
		return nil, errors.New("unexpected fields")
	}
	for _, key := range keys {
		if len(query[key]) != 1 || query.Get(key) == "" {
			return nil, fmt.Errorf("no single %s", key)
		}
	}
	return query, nil
}

func Parse(address string) (Reference, error) {
	query, err := parseQuery(address, toolResultHost, "session", "call")
	if err != nil {
		return Reference{}, fmt.Errorf("invalid tool result link: %w", err)
	}
	return Reference{SessionName: query.Get("session"), CallID: query.Get("call")}, nil
}

func Read(directory string, address string) (Exchange, error) {
	reference, err := Parse(address)
	if err != nil {
		return Exchange{}, err
	}

	var exchange Exchange
	isFound := false
	err = session.Records(directory, reference.SessionName, func(line session.Line) error {
		if line.Kind != session.Event || line.Event == nil || line.Event.ID != reference.CallID {
			return nil
		}

		if line.Event.Kind == agent.ToolCallRequestEvent {
			exchange.Request = *line.Event
			return nil
		}
		if line.Event.Kind != agent.ToolCallResultEvent {
			return nil
		}

		exchange.Result = *line.Event
		isFound = true
		return errResultFound
	})
	if err != nil && !errors.Is(err, errResultFound) {
		return Exchange{}, err
	}
	if !isFound {
		return Exchange{}, errors.New("tool result not found")
	}

	return exchange, nil
}
