package access

import (
	"errors"
	"strconv"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestLastRecordedSkipsOtherKindsAndMalformedValues(t *testing.T) {
	events := []agent.Event{
		{Kind: "access", Text: "1"},
		{Kind: "other", Text: "2"},
		{Kind: "access", Text: "broken"},
		{Kind: "access", Text: "3"},
	}

	value, found := LastRecorded(events, "access", func(event agent.Event) (int, error) {
		value, err := strconv.Atoi(event.Text)
		if err != nil {
			return 0, errors.New("malformed")
		}
		return value, nil
	})
	if !found || value != 3 {
		t.Errorf("got %d and %t", value, found)
	}
}

func TestLastRecordedReportsNoDecodableValue(t *testing.T) {
	value, found := LastRecorded([]agent.Event{{Kind: "access", Text: "broken"}}, "access", func(agent.Event) (int, error) {
		return 0, errors.New("malformed")
	})
	if found || value != 0 {
		t.Errorf("got %d and %t", value, found)
	}
}
