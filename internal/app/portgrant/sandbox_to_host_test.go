package portgrant

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/pkg/agent"
)

func recordingSandboxToHostExposer(exposed *[]uint16) SandboxToHostExposer {
	return SandboxToHostExposer{
		Expose: func(port uint16) error {
			*exposed = append(*exposed, port)
			return nil
		},
		Hide: func(port uint16) error {
			*exposed = slices.DeleteFunc(*exposed, func(openPort uint16) bool { return openPort == port })
			return nil
		},
	}
}

func TestAHostPortCanBeExposedToTheSandboxAndRevoked(t *testing.T) {
	var exposed []uint16
	grants := NewSandboxToHost(recordingSandboxToHostExposer(&exposed), nil)

	event, err := grants.Expose(8080)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(exposed, []uint16{8080}) || !slices.Equal(grants.GetCurrent(), []uint16{8080}) {
		t.Errorf("got exposer %v and grants %v, want [8080] in both", exposed, grants.GetCurrent())
	}
	if notice, isSaid := SandboxToHostNotice(event); !isSaid || notice != "Host loopback port 8080 exposed to sandbox." {
		t.Errorf("got %q, %v", notice, isSaid)
	}

	event, err = grants.Revoke(8080)
	if err != nil {
		t.Fatal(err)
	}
	if len(exposed) != 0 || len(grants.GetCurrent()) != 0 {
		t.Errorf("got exposer %v and grants %v, want none", exposed, grants.GetCurrent())
	}
	if notice, isSaid := SandboxToHostNotice(event); !isSaid || notice != "Host loopback port 8080 no longer exposed to sandbox." {
		t.Errorf("got %q, %v", notice, isSaid)
	}
}

func TestAHostPortCannotConflictWithTrafficFromTheHostToTheSandbox(t *testing.T) {
	var exposed []uint16
	grants := NewSandboxToHost(recordingSandboxToHostExposer(&exposed), func() []uint16 {
		return []uint16{8080}
	})

	if _, err := grants.Expose(8080); err == nil || !strings.Contains(err.Error(), "already exposed from the sandbox") {
		t.Errorf("got %v, want a directional conflict", err)
	}
}

func TestHostPortsRestoreAndKeepFailuresForTheModel(t *testing.T) {
	var exposed []uint16
	grants, result := NewRestoredSandboxToHost(SandboxToHostExposer{
		Expose: func(port uint16) error {
			if port == 3000 {
				return errors.New("occupied")
			}
			exposed = append(exposed, port)
			return nil
		},
		Hide: func(uint16) error { return nil },
	}, nil, []uint16{3000, 8080})

	if !slices.Equal(grants.GetCurrent(), []uint16{8080}) {
		t.Errorf("got %v, want [8080]", grants.GetCurrent())
	}
	if len(result.Failures) != 1 || result.Failures[0].Port != 3000 {
		t.Errorf("got failures %#v", result.Failures)
	}
	if message := grants.Peek(); !strings.Contains(message, "3000") || !strings.Contains(message, "no longer reachable") {
		t.Errorf("got %q, want the failed restoration", message)
	}
}

func TestAHostPortChangeCanBeRestoredFromTheJournal(t *testing.T) {
	event, err := SandboxToHostChangeEvent(8080, []uint16{8080})
	if err != nil {
		t.Fatal(err)
	}

	ports, found := LastRecordedSandboxToHost([]agent.Event{event})
	if !found || !slices.Equal(ports, []uint16{8080}) {
		t.Errorf("got %v, %v", ports, found)
	}
}
