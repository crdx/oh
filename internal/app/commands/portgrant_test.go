package commands

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/pkg/agent"
)

func fixturePortGrants() (Forwards, *[]uint16) {
	current := []uint16{}
	ports := Forwards{
		Forward: func(port uint16) (agent.Event, error) {
			current = append(current, port)
			return agent.Event{Kind: portgrant.ForwardChange, Name: "forwarded"}, nil
		},
		Revoke: func(port uint16) (agent.Event, error) {
			current = slices.DeleteFunc(current, func(open uint16) bool { return open == port })
			return agent.Event{Kind: portgrant.ForwardChange, Name: "revoked"}, nil
		},
		GetCurrent: func() []uint16 { return slices.Clone(current) },
		GetURL:     func(port uint16) string { return portgrant.URL("127.9.9.9", port) },
	}
	return ports, &current
}

func invokeGrantCommand(
	t *testing.T, grants PathGrants, ports Forwards, input string,
) (*commandTestContext, error) {
	t.Helper()

	registry := newCommandRegistry(t, commandEnvironment{pathGrants: grants, forwards: ports})
	invocation, found := registry.Find(input)
	if !found {
		t.Fatalf("did not find %s", input)
	}
	context := &commandTestContext{}
	return context, invocation.Command.Run(context, invocation.Arguments)
}

func TestForwardMakesASandboxPortReachableFromTheHost(t *testing.T) {
	grants, _ := fixturePathGrants()
	ports, forwarded := fixturePortGrants()

	context, err := invokeGrantCommand(t, grants, ports, "/forward 8080")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*forwarded, []uint16{8080}) {
		t.Errorf("got forwarded ports %v, want [8080]", *forwarded)
	}
	if len(context.events) != 1 || context.events[0].Kind != portgrant.ForwardChange {
		t.Errorf("got events %#v", context.events)
	}
}

func TestForwardNeedsAPortToForward(t *testing.T) {
	grants, _ := fixturePathGrants()
	ports, forwarded := fixturePortGrants()

	for _, input := range []string{"/forward", "/forward web", "/forward 0", "/forward 70000", "/forward -1", "/forward 8080 9090", "/forward 80.5"} {
		if _, err := invokeGrantCommand(t, grants, ports, input); err == nil {
			t.Errorf("%s was accepted", input)
		}
	}
	if len(*forwarded) != 0 {
		t.Errorf("got forwarded ports %v, want none", *forwarded)
	}
}

func TestForwardAcceptsAPortWithSurroundingSpace(t *testing.T) {
	grants, _ := fixturePathGrants()
	ports, forwarded := fixturePortGrants()

	if _, err := invokeGrantCommand(t, grants, ports, "/forward   8080  "); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*forwarded, []uint16{8080}) {
		t.Errorf("got forwarded ports %v, want [8080]", *forwarded)
	}
}

func TestForwardReportsWhyThePortCouldNotBeForwarded(t *testing.T) {
	grants, _ := fixturePathGrants()
	ports, forwarded := fixturePortGrants()
	ports.Forward = func(uint16) (agent.Event, error) {
		return agent.Event{}, errors.New("address already in use")
	}

	context, err := invokeGrantCommand(t, grants, ports, "/forward 8080")
	if err == nil || err.Error() != "address already in use" {
		t.Errorf("got %v, want the refusal", err)
	}
	if len(context.events) != 0 || len(*forwarded) != 0 {
		t.Errorf("got events %#v and ports %v, want neither", context.events, *forwarded)
	}
}

func TestForwardIsNotOfferedTheOldExposeName(t *testing.T) {
	grants, _ := fixturePathGrants()
	ports, _ := fixturePortGrants()
	registry := newCommandRegistry(t, commandEnvironment{pathGrants: grants, forwards: ports})

	if _, found := registry.Find("/expose 8080"); found {
		t.Error("the retired /expose command is still registered")
	}
	if _, found := registry.Find("/forward 8080"); !found {
		t.Error("/forward is not registered")
	}
}

func TestForwardSaysWhenThereIsNoSandboxToForwardFrom(t *testing.T) {
	grants, _ := fixturePathGrants()

	_, err := invokeGrantCommand(t, grants, Forwards{}, "/forward 8080")
	if err == nil || !strings.Contains(err.Error(), "the sandbox's own network") {
		t.Errorf("got %v, want a refusal naming the sandbox network", err)
	}
}

func TestRevokeClosesAForwardedPortAndLeavesThePathsAlone(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, forwarded := fixturePortGrants()
	*forwarded = []uint16{3000, 8080}

	context, err := invokeGrantCommand(t, grants, ports, "/revoke 8080")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*forwarded, []uint16{3000}) {
		t.Errorf("got forwarded ports %v, want [3000]", *forwarded)
	}
	if len(*paths) != 1 {
		t.Errorf("got path grants %#v, want the path left alone", *paths)
	}
	if len(context.events) != 1 || context.events[0].Kind != portgrant.ForwardChange {
		t.Errorf("got events %#v", context.events)
	}
}

func TestRevokeReadsAnythingThatIsNotAPortAsAPath(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, forwarded := fixturePortGrants()
	*forwarded = []uint16{8080}

	if _, err := invokeGrantCommand(t, grants, ports, "/revoke /reference"); err != nil {
		t.Fatal(err)
	}
	if len(*paths) != 0 {
		t.Errorf("got path grants %#v, want none", *paths)
	}
	if !slices.Equal(*forwarded, []uint16{8080}) {
		t.Errorf("got forwarded ports %v, want the port left alone", *forwarded)
	}
}

func TestRevokeClosesEveryPathAndPortItIsGiven(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{
		{Path: "/first", Access: pathgrant.ReadAccess},
		{Path: "/second", Access: pathgrant.ReadAccess},
		{Path: "/third", Access: pathgrant.ReadAccess},
	}
	ports, forwarded := fixturePortGrants()
	*forwarded = []uint16{3000, 8080}

	context, err := invokeGrantCommand(t, grants, ports, "/revoke /first 8080 /third")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pathGrantPaths(*paths), []string{"/second"}) {
		t.Errorf("got path grants %#v, want only /second left", *paths)
	}
	if !slices.Equal(*forwarded, []uint16{3000}) {
		t.Errorf("got forwarded ports %v, want [3000]", *forwarded)
	}
	if len(context.events) != 3 {
		t.Errorf("got events %#v, want one for each revocation", context.events)
	}
}

func TestRevokeClosesASubjectNamedTwiceOnce(t *testing.T) {
	grants, _ := fixturePathGrants()
	ports, forwarded := fixturePortGrants()
	*forwarded = []uint16{8080}

	context, err := invokeGrantCommand(t, grants, ports, "/revoke 8080 8080")
	if err != nil {
		t.Fatal(err)
	}
	if len(context.events) != 1 {
		t.Errorf("got events %#v, want one", context.events)
	}
}

func TestRevokeClosesWhatItCanAndNamesWhatItCannot(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}

	context, err := invokeGrantCommand(t, grants, Forwards{}, "/revoke 8080 /reference 9090")
	if err == nil || err.Error() != "port 8080 is not forwarded; port 9090 is not forwarded" {
		t.Errorf("got %v, want both ports named", err)
	}
	if len(*paths) != 0 {
		t.Errorf("got path grants %#v, want none", *paths)
	}
	if len(context.events) != 1 {
		t.Errorf("got events %#v, want the path's alone", context.events)
	}
}

func TestRevokeReadsAGrantedPathHoldingASpaceWhole(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{
		{Path: "/some", Access: pathgrant.ReadAccess},
		{Path: "/some path", Access: pathgrant.ReadAccess},
	}

	if _, err := invokeGrantCommand(t, grants, Forwards{}, "/revoke /some path"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pathGrantPaths(*paths), []string{"/some"}) {
		t.Errorf("got path grants %#v, want only /some left", *paths)
	}
}

func TestRevokeSaysAPortIsNotForwardedWhereThereIsNoSandbox(t *testing.T) {
	grants, _ := fixturePathGrants()

	_, err := invokeGrantCommand(t, grants, Forwards{}, "/revoke 8080")
	if err == nil || !strings.Contains(err.Error(), "not forwarded") {
		t.Errorf("got %v, want a refusal saying the port is not forwarded", err)
	}
}

func TestRevokeOffersEveryPathAndPortItCanClose(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, forwarded := fixturePortGrants()
	*forwarded = []uint16{8080}

	subjects := revocableSubjects(grants, ports)
	if !slices.Equal(subjects, []string{"/reference", "8080"}) {
		t.Errorf("got %v, want the path and the port", subjects)
	}
}
