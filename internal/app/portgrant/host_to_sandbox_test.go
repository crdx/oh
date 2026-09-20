package portgrant

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/pkg/agent"
)

const testHost = "127.0.0.1"

func recordingExposer(exposed *[]uint16) HostToSandboxExposer {
	return HostToSandboxExposer{
		Expose: func(port uint16) error {
			*exposed = append(*exposed, port)
			return nil
		},
		Hide: func(port uint16) error {
			*exposed = slices.DeleteFunc(*exposed, func(open uint16) bool { return open == port })
			return nil
		},
	}
}

func TestExposingAPortOpensItAndRecordsTheWholeSet(t *testing.T) {
	var exposed []uint16
	ports := NewHostToSandbox(recordingExposer(&exposed), testHost)

	if _, err := ports.Expose(8080); err != nil {
		t.Fatal(err)
	}
	event, err := ports.Expose(3000)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(exposed, []uint16{8080, 3000}) {
		t.Errorf("got opened ports %v, want [8080 3000]", exposed)
	}
	if !slices.Equal(ports.GetCurrent(), []uint16{3000, 8080}) {
		t.Errorf("got current ports %v, want [3000 8080]", ports.GetCurrent())
	}
	if event.Name != "3000" {
		t.Errorf("got event name %q, want the port that changed", event.Name)
	}

	recorded, found := LastRecordedHostToSandbox([]agent.Event{event})
	if !found || !slices.Equal(recorded, []uint16{3000, 8080}) {
		t.Errorf("got recorded ports %v and %t, want the whole set", recorded, found)
	}
}

func TestHidingAPortClosesItAndLeavesTheRestBehind(t *testing.T) {
	var exposed []uint16
	ports := NewHostToSandbox(recordingExposer(&exposed), testHost)

	if _, err := ports.Expose(8080); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Expose(3000); err != nil {
		t.Fatal(err)
	}
	event, err := ports.Hide(8080)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(exposed, []uint16{3000}) {
		t.Errorf("got opened ports %v, want [3000]", exposed)
	}
	recorded, found := LastRecordedHostToSandbox([]agent.Event{event})
	if !found || !slices.Equal(recorded, []uint16{3000}) {
		t.Errorf("got recorded ports %v and %t, want [3000]", recorded, found)
	}
	if _, err := ports.Hide(8080); err == nil {
		t.Error("hiding a port that was not exposed was allowed")
	}
}

func TestAPortIsRefusedWhenItCannotBeExposed(t *testing.T) {
	var exposed []uint16
	ports := NewHostToSandbox(recordingExposer(&exposed), testHost)
	ports.SetSandboxToHostPorts(func() []uint16 { return []uint16{80} })

	for name, port := range map[string]uint16{
		"zero":      0,
		"reserved":  1023,
		"forwarded": 80,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ports.Expose(port); err == nil {
				t.Errorf("port %d was exposed", port)
			}
		})
	}

	if _, err := ports.Expose(8080); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Expose(8080); err == nil {
		t.Error("a port was exposed twice")
	}
	if len(exposed) != 1 {
		t.Errorf("got opened ports %v, want only the one that was allowed", exposed)
	}
}

func TestExposingIsRefusedWithoutASandboxToExposeFrom(t *testing.T) {
	ports := NewHostToSandbox(HostToSandboxExposer{}, testHost)

	_, err := ports.Expose(8080)
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("got %v, want a refusal naming the sandbox", err)
	}
}

func TestARestoredSessionOpensThePortsItRecordedAndReportsTheRest(t *testing.T) {
	refused := errors.New("address already in use")
	ports, result := NewRestoredHostToSandbox(HostToSandboxExposer{
		Expose: func(port uint16) error {
			if port == 3000 {
				return refused
			}
			return nil
		},
		Hide: func(uint16) error { return nil },
	}, testHost, []uint16{3000, 8080})

	if !slices.Equal(ports.GetCurrent(), []uint16{8080}) {
		t.Errorf("got current ports %v, want [8080]", ports.GetCurrent())
	}
	if len(result.Failures) != 1 || result.Failures[0].Port != 3000 {
		t.Fatalf("got failures %v, want the port that could not be opened", result.Failures)
	}
	if !errors.Is(result.Failures[0].Err, refused) {
		t.Errorf("got %v, want the reason the port was refused", result.Failures[0].Err)
	}
}

func TestARestoredSessionSaysNothingAboutThePortsItWasAlreadyToldOf(t *testing.T) {
	var exposed []uint16
	ports, _ := NewRestoredHostToSandbox(recordingExposer(&exposed), testHost, []uint16{8080})

	if told := ports.Peek(); told != "" {
		t.Errorf("got %q, want nothing said about a restored port", told)
	}
	if _, err := ports.Expose(3000); err != nil {
		t.Fatal(err)
	}
	if told := ports.Inject(); !strings.Contains(told, "3000") || strings.Contains(told, "8080") {
		t.Errorf("got %q, want only the newly exposed port", told)
	}
}

func TestTheModelIsToldWhenAPortOpensAndWhenItCloses(t *testing.T) {
	var exposed []uint16
	ports := NewHostToSandbox(recordingExposer(&exposed), testHost)

	if _, err := ports.Expose(8080); err != nil {
		t.Fatal(err)
	}
	told := ports.Inject()
	if !strings.Contains(told, "8080") || !strings.Contains(told, URL(testHost, 8080)) {
		t.Errorf("got %q, want the port and the address it is reachable at", told)
	}

	if _, err := ports.Hide(8080); err != nil {
		t.Fatal(err)
	}
	told = ports.Inject()
	if !strings.Contains(told, "no longer reachable") {
		t.Errorf("got %q, want the port to be said to have gone", told)
	}
}

func TestANoticeSaysWhichWayThePortWent(t *testing.T) {
	opened, err := HostToSandboxChangeEvent(testHost, 8080, []uint16{8080})
	if err != nil {
		t.Fatal(err)
	}
	notice, isSaid := HostToSandboxNotice(opened)
	if !isSaid || notice != "Sandbox port 8080 exposed at "+URL(testHost, 8080)+"." {
		t.Errorf("got %q and %t", notice, isSaid)
	}

	closed, err := HostToSandboxChangeEvent(testHost, 8080, nil)
	if err != nil {
		t.Fatal(err)
	}
	notice, isSaid = HostToSandboxNotice(closed)
	if !isSaid || notice != "Sandbox port 8080 no longer exposed to host." {
		t.Errorf("got %q and %t", notice, isSaid)
	}

	if _, isSaid := HostToSandboxNotice(agent.Event{Kind: HostToSandboxChange}); isSaid {
		t.Error("an event naming no port was rendered")
	}
	if _, isSaid := HostToSandboxNotice(agent.Event{Kind: "other", Name: "8080"}); isSaid {
		t.Error("an event of another kind was rendered")
	}
}

func TestASummaryCountsTheExposedPorts(t *testing.T) {
	for name, shape := range map[string]struct {
		ports []uint16
		want  string
	}{
		"none": {ports: nil, want: "none"},
		"one":  {ports: []uint16{8080}, want: "1 sandbox port"},
		"two":  {ports: []uint16{3000, 8080}, want: "2 sandbox ports"},
	} {
		t.Run(name, func(t *testing.T) {
			event, err := HostToSandboxChangeEvent(testHost, 8080, shape.ports)
			if err != nil {
				t.Fatal(err)
			}
			summary, isSaid := HostToSandboxSummary(event)
			if !isSaid || summary != shape.want {
				t.Errorf("got %q and %t, want %q", summary, isSaid, shape.want)
			}
		})
	}
}

func TestInvalidStateIsNotReadBackAsAnEmptyGrant(t *testing.T) {
	if _, found := LastRecordedHostToSandbox([]agent.Event{{Kind: HostToSandboxChange, State: []byte(`{"ports":[0]}`)}}); found {
		t.Error("state naming port 0 was restored")
	}
	if _, found := LastRecordedHostToSandbox([]agent.Event{{Kind: HostToSandboxChange, State: []byte(`not json`)}}); found {
		t.Error("malformed state was restored")
	}
}

func TestAPortIsParsedOnlyWhenItIsOne(t *testing.T) {
	port, err := ParsePort(" 8080 ")
	if err != nil || port != 8080 {
		t.Errorf("got %d and %v", port, err)
	}
	for _, written := range []string{"", "0", "-1", "65536", "http", "80 80"} {
		if _, err := ParsePort(written); err == nil {
			t.Errorf("%q was read as a port", written)
		}
	}
}

func TestAnAddressIsDerivedFromTheSessionNameAndIsAlwaysLoopback(t *testing.T) {
	for _, sessionName := range []string{"tame-impala", "ornate-grouse", "", "a"} {
		address, err := netip.ParseAddr(AddressFor(sessionName))
		if err != nil {
			t.Fatalf("%q gave %v", sessionName, err)
		}
		if !address.IsLoopback() {
			t.Errorf("%q gave %s, want a loopback address", sessionName, address)
		}
		if last := address.As4()[3]; last == 0 || last == 255 {
			t.Errorf("%q gave %s, want a usable host octet", sessionName, address)
		}
		if address.String() == LocalHost {
			t.Errorf("%q gave the standard loopback address, want one of its own", sessionName)
		}
		if again := AddressFor(sessionName); again != address.String() {
			t.Errorf("%q gave %s and then %s, want the same address on resume", sessionName, address, again)
		}
	}

	if AddressFor("tame-impala") == AddressFor("ornate-grouse") {
		t.Error("two sessions were given the same address")
	}
}

func TestWhatTheModelExposesIsAnnouncedToTheHarness(t *testing.T) {
	var exposed []uint16
	ports := NewHostToSandbox(recordingExposer(&exposed), testHost)
	access := ports.ForModel()

	address, err := access.Expose(8080)
	if err != nil {
		t.Fatal(err)
	}
	if address != URL(testHost, 8080) {
		t.Errorf("got %q, want the address the user can open", address)
	}
	if publications := access.List(); len(publications) != 1 || publications[0].Port != 8080 {
		t.Errorf("got %#v, want the exposed port", publications)
	}

	event := <-ports.Changes()
	if event.Kind != HostToSandboxChange || event.Name != "8080" {
		t.Errorf("got %#v, want the port that was exposed", event)
	}

	if err := access.Hide(8080); err != nil {
		t.Fatal(err)
	}
	if event := <-ports.Changes(); event.Kind != HostToSandboxChange || event.Name != "8080" {
		t.Errorf("got %#v, want the port that was hidden", event)
	}
	if publications := access.List(); len(publications) != 0 {
		t.Errorf("got %#v, want nothing exposed", publications)
	}
}

func TestWhatTheModelCannotExposeIsNotAnnounced(t *testing.T) {
	var exposed []uint16
	ports := NewHostToSandbox(recordingExposer(&exposed), testHost)
	access := ports.ForModel()

	if _, err := access.Expose(80); err == nil {
		t.Fatal("a reserved port was exposed")
	}
	if err := access.Hide(8080); err == nil {
		t.Fatal("a port that was not exposed was closed")
	}

	select {
	case event := <-ports.Changes():
		t.Errorf("got %#v, want nothing announced", event)
	default:
	}
}
