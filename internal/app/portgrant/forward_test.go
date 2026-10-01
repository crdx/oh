package portgrant

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

const testHost = "127.0.0.1"

func recordingForwarder(forwarded *[]uint16) Forwarder {
	return Forwarder{
		Forward: func(port uint16) error {
			*forwarded = append(*forwarded, port)
			return nil
		},
		Revoke: func(port uint16) error {
			*forwarded = slices.DeleteFunc(*forwarded, func(open uint16) bool { return open == port })
			return nil
		},
	}
}

func TestForwardingAPortOpensItAndRecordsTheWholeSet(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	if _, err := ports.Forward(8080); err != nil {
		t.Fatal(err)
	}
	event, err := ports.Forward(3000)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(forwarded, []uint16{8080, 3000}) {
		t.Errorf("got opened ports %v, want [8080 3000]", forwarded)
	}
	if !slices.Equal(ports.GetCurrent(), []uint16{3000, 8080}) {
		t.Errorf("got current ports %v, want [3000 8080]", ports.GetCurrent())
	}
	if event.Name != "3000" {
		t.Errorf("got event name %q, want the port that changed", event.Name)
	}

	recorded, found := LastRecordedForwards([]agent.Event{event})
	if !found || !slices.Equal(recorded, []Route{{Port: 3000}, {Port: 8080}}) {
		t.Errorf("got recorded ports %v and %t, want the whole set", recorded, found)
	}
}

func TestRevokingAPortClosesItAndLeavesTheRestBehind(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	if _, err := ports.Forward(8080); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Forward(3000); err != nil {
		t.Fatal(err)
	}
	event, err := ports.Revoke(8080)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(forwarded, []uint16{3000}) {
		t.Errorf("got opened ports %v, want [3000]", forwarded)
	}
	recorded, found := LastRecordedForwards([]agent.Event{event})
	if !found || !slices.Equal(recorded, []Route{{Port: 3000}}) {
		t.Errorf("got recorded ports %v and %t, want [3000]", recorded, found)
	}
	if _, err := ports.Revoke(8080); err == nil {
		t.Error("revoking a port that was not forwarded was allowed")
	}
}

func TestAPortIsRefusedWhenItCannotBeForwarded(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	for name, port := range map[string]uint16{
		"zero":     0,
		"reserved": 1023,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ports.Forward(port); err == nil {
				t.Errorf("port %d was forwarded", port)
			}
		})
	}

	if _, err := ports.Forward(8080); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Forward(8080); err == nil {
		t.Error("a port was forwarded twice")
	}
	if len(forwarded) != 1 {
		t.Errorf("got opened ports %v, want only the one that was allowed", forwarded)
	}
}

func TestForwardingIsRefusedWithoutASandboxToForwardFrom(t *testing.T) {
	ports := NewForwards(Forwarder{}, testHost)

	_, err := ports.Forward(8080)
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("got %v, want a refusal naming the sandbox", err)
	}
}

func TestARestoredSessionOpensThePortsItRecordedAndReportsTheRest(t *testing.T) {
	refused := errors.New("address already in use")
	ports, result := NewRestoredForwards(Forwarder{
		Forward: func(port uint16) error {
			if port == 3000 {
				return refused
			}
			return nil
		},
		Revoke: func(uint16) error { return nil },
	}, testHost, []Route{{Port: 3000}, {Port: 8080, JobName: "docs"}})

	if !slices.Equal(ports.GetCurrent(), []uint16{8080}) {
		t.Errorf("got current ports %v, want [8080]", ports.GetCurrent())
	}
	if routes := ports.GetRoutes(); !slices.Equal(routes, []Route{{Port: 8080, JobName: "docs"}}) {
		t.Errorf("got routes %#v, want the restored job association", routes)
	}
	if len(result.Failures) != 1 || result.Failures[0].Port != 3000 {
		t.Fatalf("got failures %v, want the port that could not be opened", result.Failures)
	}
	if !errors.Is(result.Failures[0].Err, refused) {
		t.Errorf("got %v, want the reason the port was refused", result.Failures[0].Err)
	}
}

func TestARestoredSessionSaysNothingAboutThePortsItWasAlreadyToldOf(t *testing.T) {
	var forwarded []uint16
	ports, _ := NewRestoredForwards(recordingForwarder(&forwarded), testHost, []Route{{Port: 8080, JobName: "docs"}})

	if told := ports.Peek(); told != "" {
		t.Errorf("got %q, want nothing said about a restored port", told)
	}
	if _, err := ports.Forward(3000); err != nil {
		t.Fatal(err)
	}
	if told := ports.Inject(); !strings.Contains(told, "3000") || strings.Contains(told, "8080") {
		t.Errorf("got %q, want only the newly forwarded port", told)
	}
}

func TestTheModelIsToldWhenAPortOpensAndWhenItCloses(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	if _, err := ports.Forward(8080); err != nil {
		t.Fatal(err)
	}
	told := ports.Inject()
	if !strings.Contains(told, "8080") || !strings.Contains(told, URL(testHost, 8080)) {
		t.Errorf("got %q, want the port and the address it is reachable at", told)
	}

	if _, err := ports.Revoke(8080); err != nil {
		t.Fatal(err)
	}
	told = ports.Inject()
	if !strings.Contains(told, "no longer reachable") {
		t.Errorf("got %q, want the port to be said to have gone", told)
	}
}

func TestANoticeSaysWhetherThePortIsForwarded(t *testing.T) {
	opened, err := ForwardChangeEvent(testHost, 8080, []Route{{Port: 8080}})
	if err != nil {
		t.Fatal(err)
	}
	notice, isSaid := ForwardNotice(opened)
	if !isSaid || notice != "Port 8080 is now forwarded to "+URL(testHost, 8080)+"." {
		t.Errorf("got %q and %t", notice, isSaid)
	}

	closed, err := ForwardChangeEvent(testHost, 8080, nil)
	if err != nil {
		t.Fatal(err)
	}
	notice, isSaid = ForwardNotice(closed)
	if !isSaid || notice != "Port 8080 is no longer forwarded." {
		t.Errorf("got %q and %t", notice, isSaid)
	}

	if _, isSaid := ForwardNotice(agent.Event{Kind: ForwardChange}); isSaid {
		t.Error("an event naming no port was rendered")
	}
	if _, isSaid := ForwardNotice(agent.Event{Kind: "other", Name: "8080"}); isSaid {
		t.Error("an event of another kind was rendered")
	}
}

func TestASummaryCountsTheForwardedPorts(t *testing.T) {
	for name, shape := range map[string]struct {
		ports []uint16
		want  string
	}{
		"none": {ports: nil, want: "none"},
		"one":  {ports: []uint16{8080}, want: "1 forwarded port"},
		"two":  {ports: []uint16{3000, 8080}, want: "2 forwarded ports"},
	} {
		t.Run(name, func(t *testing.T) {
			event, err := ForwardChangeEvent(testHost, 8080, routesForPorts(shape.ports...))
			if err != nil {
				t.Fatal(err)
			}
			summary, isSaid := ForwardSummary(event)
			if !isSaid || summary != shape.want {
				t.Errorf("got %q and %t, want %q", summary, isSaid, shape.want)
			}
		})
	}
}

func TestAnOlderPortEventRestoresWithoutAJobAssociation(t *testing.T) {
	event := agent.Event{
		Kind:  ForwardChange,
		State: []byte(`{"host":"127.9.9.9","ports":[8080]}`),
	}

	routes, found := LastRecordedForwards([]agent.Event{event})
	if !found || !slices.Equal(routes, []Route{{Port: 8080}}) {
		t.Errorf("got routes %#v and %t, want the unassociated port", routes, found)
	}
}

func TestInvalidStateIsNotReadBackAsAnEmptyGrant(t *testing.T) {
	if _, found := LastRecordedForwards([]agent.Event{{Kind: ForwardChange, State: []byte(`{"ports":[0]}`)}}); found {
		t.Error("state naming port 0 was restored")
	}
	if _, found := LastRecordedForwards([]agent.Event{{Kind: ForwardChange, State: []byte(`not json`)}}); found {
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

func TestWhatTheModelForwardsIsAnnouncedToTheHarness(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)
	access := ports.ForModel()

	address, err := access.Forward(8080, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if address != URL(testHost, 8080) {
		t.Errorf("got %q, want the address the user can open", address)
	}
	if publications := access.List(); len(publications) != 1 || publications[0].Port != 8080 || publications[0].JobName != "docs" {
		t.Errorf("got %#v, want the forwarded port", publications)
	}

	event := <-ports.Changes()
	if event.Kind != ForwardChange || event.Name != "8080" {
		t.Errorf("got %#v, want the port that was forwarded", event)
	}
	recorded, found := LastRecordedForwards([]agent.Event{event})
	if !found || !slices.Equal(recorded, []Route{{Port: 8080, JobName: "docs"}}) {
		t.Errorf("got recorded routes %#v and %t, want the job association", recorded, found)
	}

	if err := access.Revoke(8080); err != nil {
		t.Fatal(err)
	}
	if event := <-ports.Changes(); event.Kind != ForwardChange || event.Name != "8080" {
		t.Errorf("got %#v, want the port that was revoked", event)
	}
	if publications := access.List(); len(publications) != 0 {
		t.Errorf("got %#v, want nothing forwarded", publications)
	}
}

func TestWhatTheModelCannotForwardIsNotAnnounced(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)
	access := ports.ForModel()

	if _, err := access.Forward(80, ""); err == nil {
		t.Fatal("a reserved port was forwarded")
	}
	if err := access.Revoke(8080); err == nil {
		t.Fatal("a port that was not forwarded was closed")
	}

	select {
	case event := <-ports.Changes():
		t.Errorf("got %#v, want nothing announced", event)
	default:
	}
}

func TestTheModelForwardingAPortAlreadyForwardedForTheSameJobChangesNothing(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)
	access := ports.ForModel()

	if _, err := access.Forward(8080, "docs"); err != nil {
		t.Fatal(err)
	}
	<-ports.Changes()

	address, err := access.Forward(8080, "docs")
	if err != nil {
		t.Fatalf("got %v, want the standing forward reused", err)
	}
	if address != URL(testHost, 8080) {
		t.Errorf("got %q, want the address the user can open", address)
	}
	if !slices.Equal(forwarded, []uint16{8080}) {
		t.Errorf("got opened ports %v, want 8080 opened once", forwarded)
	}
	select {
	case event := <-ports.Changes():
		t.Errorf("got %#v, want nothing announced", event)
	default:
	}
}

func TestTheModelForwardingAPortAlreadyForwardedForAnotherJobIsRefused(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)
	access := ports.ForModel()

	if _, err := access.Forward(8080, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Forward(8080, "api"); err == nil {
		t.Fatal("a port forwarded for one job was taken by another")
	}
}

func TestTheModelStartingAJobOnAPortForwardedForNoJobTakesTheForwardOver(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)
	access := ports.ForModel()

	if _, err := access.Forward(8080, ""); err != nil {
		t.Fatal(err)
	}
	<-ports.Changes()

	address, err := access.Forward(8080, "docs")
	if err != nil {
		t.Fatalf("got %v, want the unclaimed forward taken over", err)
	}
	if address != URL(testHost, 8080) {
		t.Errorf("got %q, want the address the user can open", address)
	}
	if !slices.Equal(forwarded, []uint16{8080}) {
		t.Errorf("got opened ports %v, want 8080 opened once", forwarded)
	}

	event := <-ports.Changes()
	recorded, found := LastRecordedForwards([]agent.Event{event})
	if !found || !slices.Equal(recorded, []Route{{Port: 8080, JobName: "docs"}}) {
		t.Errorf("got recorded routes %#v and %t, want the job association", recorded, found)
	}
	if notice, _ := ForwardNotice(event); notice != "Port 8080 now belongs to job docs." {
		t.Errorf("got notice %q, want the port handed to the job", notice)
	}
}

func TestAForwardHeldByAJobIsNotGivenUpToNoJob(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)
	access := ports.ForModel()

	if _, err := access.Forward(8080, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Forward(8080, ""); err == nil {
		t.Fatal("a port forwarded for a job was taken back from it")
	}
}

func routesForPorts(ports ...uint16) []Route {
	routes := make([]Route, 0, len(ports))
	for _, port := range ports {
		routes = append(routes, Route{Port: port})
	}

	return routes
}

func TestAPortAtTheLowestBindableNumberIsForwarded(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	if _, err := ports.Forward(lowestPort - 1); err == nil {
		t.Errorf("port %d was forwarded", lowestPort-1)
	}
	if _, err := ports.Forward(lowestPort); err != nil {
		t.Errorf("port %d was refused: %v", lowestPort, err)
	}
	if _, err := ports.Forward(65535); err != nil {
		t.Errorf("port 65535 was refused: %v", err)
	}
}

func TestAPortTheKeeperCannotOpenIsNeitherRecordedNorTold(t *testing.T) {
	refused := errors.New("address already in use")
	ports := NewForwards(Forwarder{
		Forward: func(uint16) error { return refused },
		Revoke:  func(uint16) error { return nil },
	}, testHost)

	if _, err := ports.Forward(8080); !errors.Is(err, refused) {
		t.Errorf("got %v, want the keeper's own refusal", err)
	}
	if len(ports.GetCurrent()) != 0 {
		t.Errorf("got current ports %v, want none", ports.GetCurrent())
	}
	if told := ports.Inject(); told != "" {
		t.Errorf("the model was told of a port that never opened: %q", told)
	}
	if len(ports.changes) != 0 {
		t.Errorf("a port that never opened was announced")
	}
}

func TestAPortTheKeeperCannotCloseStaysForwarded(t *testing.T) {
	refused := errors.New("the keeper went away")
	var isRefusing bool
	ports := NewForwards(Forwarder{
		Forward: func(uint16) error { return nil },
		Revoke: func(uint16) error {
			if isRefusing {
				return refused
			}
			return nil
		},
	}, testHost)
	if _, err := ports.Forward(8080); err != nil {
		t.Fatal(err)
	}
	ports.Inject()

	isRefusing = true
	if _, err := ports.Revoke(8080); !errors.Is(err, refused) {
		t.Errorf("got %v, want the keeper's own refusal", err)
	}
	if !slices.Equal(ports.GetCurrent(), []uint16{8080}) {
		t.Errorf("got current ports %v, want the port left forwarded", ports.GetCurrent())
	}
	if told := ports.Inject(); told != "" {
		t.Errorf("the model was told of a change that did not happen: %q", told)
	}
}

func TestOnlyASetNumberOfPortsMayBeForwardedAtOnce(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	for offset := range maxForwardedPorts {
		if _, err := ports.Forward(uint16(8000 + offset)); err != nil {
			t.Fatalf("port %d was refused below the limit: %v", 8000+offset, err)
		}
	}
	if _, err := ports.Forward(9000); err == nil || !strings.Contains(err.Error(), "as many as a session may open") {
		t.Errorf("got %v, want the limit named", err)
	}

	if _, err := ports.Revoke(8000); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Forward(9000); err != nil {
		t.Errorf("a port was refused after room was made: %v", err)
	}
}

func TestARevokedPortCanBeForwardedAgain(t *testing.T) {
	var forwarded []uint16
	ports := NewForwards(recordingForwarder(&forwarded), testHost)

	for range 2 {
		if _, err := ports.Forward(8080); err != nil {
			t.Fatal(err)
		}
		if _, err := ports.Revoke(8080); err != nil {
			t.Fatal(err)
		}
	}
	if len(forwarded) != 0 {
		t.Errorf("got open ports %v, want none", forwarded)
	}
}

func TestOnlyAForwardChangeIsRestoredFromTheJournal(t *testing.T) {
	forwarded, err := ForwardChangeEvent(testHost, 8080, []Route{{Port: 8080}})
	if err != nil {
		t.Fatal(err)
	}
	other := agent.Event{Kind: "sandbox_to_host_change", Name: "3000", State: []byte(`{"ports":[3000]}`)}

	recorded, found := LastRecordedForwards([]agent.Event{forwarded, other})
	if !found || !slices.Equal(recorded, []Route{{Port: 8080}}) {
		t.Errorf("got %v and %t, want the forward alone", recorded, found)
	}
	if _, found := LastRecordedForwards([]agent.Event{other}); found {
		t.Error("an event of another kind was read as a forward")
	}
	if _, isSaid := ForwardNotice(other); isSaid {
		t.Error("an event of another kind was rendered as a forward")
	}
}
