package commands

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/pkg/agent"
)

func fixturePortGrants() (HostToSandbox, *[]uint16) {
	current := []uint16{}
	ports := HostToSandbox{
		Hide: func(port uint16) (agent.Event, error) {
			current = slices.DeleteFunc(current, func(open uint16) bool { return open == port })
			return agent.Event{Kind: portgrant.HostToSandboxChange, Name: "hidden"}, nil
		},
		GetCurrent: func() []uint16 { return slices.Clone(current) },
		GetURL:     func(port uint16) string { return portgrant.URL("127.9.9.9", port) },
	}
	return ports, &current
}

func fixtureSandboxToHost() (SandboxToHost, *[]uint16) {
	current := []uint16{}
	ports := SandboxToHost{
		Expose: func(port uint16) (agent.Event, error) {
			current = append(current, port)
			return agent.Event{Kind: portgrant.SandboxToHostChange, Name: "exposed"}, nil
		},
		Revoke: func(port uint16) (agent.Event, error) {
			current = slices.DeleteFunc(current, func(openPort uint16) bool { return openPort == port })
			return agent.Event{Kind: portgrant.SandboxToHostChange, Name: "revoked"}, nil
		},
		GetCurrent: func() []uint16 { return slices.Clone(current) },
	}
	return ports, &current
}

func invokeGrantCommand(
	t *testing.T, grants PathGrants, ports HostToSandbox, input string,
) (*commandTestContext, error) {
	t.Helper()

	registry := newCommandRegistry(t, commandEnvironment{pathGrants: grants, hostToSandbox: ports})
	invocation, found := registry.Find(input)
	if !found {
		t.Fatalf("did not find %s", input)
	}
	context := &commandTestContext{}
	return context, invocation.Command.Run(context, invocation.Arguments)
}

func TestExposeMakesAHostPortReachableFromTheSandbox(t *testing.T) {
	grants, _ := fixturePathGrants()
	sandboxToHost, exposed := fixtureSandboxToHost()
	registry := newCommandRegistry(t, commandEnvironment{
		pathGrants:    grants,
		sandboxToHost: sandboxToHost,
	})
	invocation, found := registry.Find("/expose 8080")
	if !found {
		t.Fatal("did not find /expose")
	}
	context := &commandTestContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*exposed, []uint16{8080}) {
		t.Errorf("got %v, want [8080]", *exposed)
	}
	if len(context.events) != 1 || context.events[0].Kind != portgrant.SandboxToHostChange {
		t.Errorf("got events %#v", context.events)
	}
}

func TestRevokePrefersAHostPortExposedToTheSandbox(t *testing.T) {
	grants, _ := fixturePathGrants()
	hostToSandbox, hostExposed := fixturePortGrants()
	sandboxToHost, sandboxExposed := fixtureSandboxToHost()
	*hostExposed = []uint16{3000}
	*sandboxExposed = []uint16{8080}

	event, err := revoke(grants, hostToSandbox, sandboxToHost, "8080")
	if err != nil {
		t.Fatal(err)
	}
	if len(*sandboxExposed) != 0 || !slices.Equal(*hostExposed, []uint16{3000}) {
		t.Errorf("got sandbox-to-host %v and host-to-sandbox %v", *sandboxExposed, *hostExposed)
	}
	if event.Kind != portgrant.SandboxToHostChange {
		t.Errorf("got event %#v", event)
	}
}

func TestRevokeClosesAnExposedPortAndLeavesThePathsAlone(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, exposed := fixturePortGrants()
	*exposed = []uint16{3000, 8080}

	context, err := invokeGrantCommand(t, grants, ports, "/revoke 8080")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*exposed, []uint16{3000}) {
		t.Errorf("got exposed ports %v, want [3000]", *exposed)
	}
	if len(*paths) != 1 {
		t.Errorf("got path grants %#v, want the path left alone", *paths)
	}
	if len(context.events) != 1 || context.events[0].Kind != portgrant.HostToSandboxChange {
		t.Errorf("got events %#v", context.events)
	}
}

func TestRevokeReadsAnythingThatIsNotAPortAsAPath(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, exposed := fixturePortGrants()
	*exposed = []uint16{8080}

	if _, err := invokeGrantCommand(t, grants, ports, "/revoke /reference"); err != nil {
		t.Fatal(err)
	}
	if len(*paths) != 0 {
		t.Errorf("got path grants %#v, want none", *paths)
	}
	if !slices.Equal(*exposed, []uint16{8080}) {
		t.Errorf("got exposed ports %v, want the port left alone", *exposed)
	}
}

func TestRevokeSaysAPortIsNotExposedWhereThereIsNoSandbox(t *testing.T) {
	grants, _ := fixturePathGrants()

	_, err := invokeGrantCommand(t, grants, HostToSandbox{}, "/revoke 8080")
	if err == nil || !strings.Contains(err.Error(), "not exposed") {
		t.Errorf("got %v, want a refusal saying the port is not exposed", err)
	}
}

func TestRevokeOffersEveryPathAndPortItCanClose(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	ports, exposed := fixturePortGrants()
	*exposed = []uint16{8080}

	subjects := revocableSubjects(grants, ports, SandboxToHost{})
	if !slices.Equal(subjects, []string{"/reference", "8080"}) {
		t.Errorf("got %v, want the path and the port", subjects)
	}
}
