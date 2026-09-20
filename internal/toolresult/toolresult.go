package toolresult

import (
	"errors"
	"fmt"
	"net/url"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

const (
	scheme = "oh"
	host   = "tool-result"
)

var errResultFound = errors.New("tool result found")

type Reference struct {
	SessionName string
	CallID      string
}

type Exchange struct {
	Request agent.Event
	Result  agent.Event
}

func URL(sessionName string, callID string) string {
	target := url.URL{Scheme: scheme, Host: host}
	query := url.Values{}
	query.Set("session", sessionName)
	query.Set("call", callID)
	target.RawQuery = query.Encode()
	return target.String()
}

func Parse(address string) (Reference, error) {
	target, err := url.Parse(address)
	if err != nil {
		return Reference{}, fmt.Errorf("invalid tool result link: %w", err)
	}
	if target.Scheme != scheme || target.Host != host || (target.Path != "" && target.Path != "/") || target.Fragment != "" || target.User != nil {
		return Reference{}, errors.New("invalid tool result link")
	}

	query := target.Query()
	if len(query) != 2 || len(query["session"]) != 1 || len(query["call"]) != 1 || query.Get("session") == "" || query.Get("call") == "" {
		return Reference{}, errors.New("invalid tool result link")
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
