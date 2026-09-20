package model

import (
	"testing"

	"crdx.org/io/pkg/agent"
)

func TestFastModeStateRoundTrips(t *testing.T) {
	for _, isFast := range []bool{false, true} {
		readBack, isFound := LastRecordedFastMode([]agent.Event{FastModeEvent(isFast)})
		if !isFound || readBack != isFast {
			t.Errorf("got fast=%t found=%t, want fast=%t", readBack, isFound, isFast)
		}
	}
}

func TestTheLastDecodableFastModeStateWins(t *testing.T) {
	events := []agent.Event{
		FastModeEvent(false),
		{Kind: agent.StateChangeEvent, Name: "another-state", State: []byte(`true`)},
		FastModeEvent(true),
		{Kind: agent.StateChangeEvent, Name: FastModeStateKey, State: []byte(`broken`)},
	}

	isFast, isFound := LastRecordedFastMode(events)
	if !isFound || !isFast {
		t.Errorf("got fast=%t found=%t", isFast, isFound)
	}
}

func TestAnUnrecordedFastModeIsStandard(t *testing.T) {
	isFast, isFound := LastRecordedFastMode(nil)
	if isFound || isFast {
		t.Errorf("got fast=%t found=%t", isFast, isFound)
	}
}
