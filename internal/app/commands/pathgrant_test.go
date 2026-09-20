package commands

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/pkg/agent"
)

func fixturePathGrants() (PathGrants, *[]pathgrant.Grant) {
	current := []pathgrant.Grant{}
	grants := PathGrants{
		Grant: func(path string, access pathgrant.Access) (agent.Event, error) {
			current = append(current, pathgrant.Grant{Path: path, Access: access})
			return agent.Event{Kind: pathgrant.Change, Name: path}, nil
		},
		Revoke: func(path string) (agent.Event, error) {
			current = slices.DeleteFunc(current, func(grant pathgrant.Grant) bool { return grant.Path == path })
			return agent.Event{Kind: pathgrant.Change, Name: path}, nil
		},
		GetCurrent: func() []pathgrant.Grant { return slices.Clone(current) },
	}
	return grants, &current
}

func invokePathGrantCommand(t *testing.T, grants PathGrants, input string) (*commandTestContext, error) {
	t.Helper()

	registry := newCommandRegistry(t, commandEnvironment{pathGrants: grants})
	invocation, found := registry.Find(input)
	if !found {
		t.Fatalf("did not find %s", input)
	}
	context := &commandTestContext{}
	return context, invocation.Command.Run(context, invocation.Arguments)
}

func TestGrantCommandPreservesSpacesInThePath(t *testing.T) {
	grants, current := fixturePathGrants()
	context, err := invokePathGrantCommand(t, grants, "/grant rw /reference/path with spaces")
	if err != nil {
		t.Fatal(err)
	}
	want := []pathgrant.Grant{{Path: "/reference/path with spaces", Access: pathgrant.ReadAccess | pathgrant.WriteAccess}}
	if !slices.Equal(*current, want) {
		t.Errorf("got grants %#v", *current)
	}
	if len(context.events) != 1 || context.events[0].Kind != pathgrant.Change {
		t.Errorf("got events %#v", context.events)
	}
}

func TestGrantCommandPassesOnAnExecutableGrant(t *testing.T) {
	grants, current := fixturePathGrants()
	context, err := invokePathGrantCommand(t, grants, "/grant rx /reference")
	if err != nil {
		t.Fatal(err)
	}
	want := []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess | pathgrant.ExecAccess}}
	if !slices.Equal(*current, want) {
		t.Errorf("got grants %#v", *current)
	}
	if len(context.events) != 1 || context.events[0].Kind != pathgrant.Change {
		t.Errorf("got events %#v", context.events)
	}
}

func TestGrantCommandRejectsAnUnknownAccess(t *testing.T) {
	grants, _ := fixturePathGrants()
	_, err := invokePathGrantCommand(t, grants, "/grant rwz /reference")
	if !slash.IsUsageError(err) {
		t.Errorf("got %v", err)
	}
}

func TestGrantsCommandListsTheCurrentState(t *testing.T) {
	grants, current := fixturePathGrants()
	*current = []pathgrant.Grant{
		{Path: "/read", Access: pathgrant.ReadAccess},
		{Path: "/write", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
		{Path: "/tools", Access: pathgrant.ReadAccess | pathgrant.WriteAccess | pathgrant.ExecAccess},
	}
	context, err := invokePathGrantCommand(t, grants, "/grants")
	if err != nil {
		t.Fatal(err)
	}
	want := "Temporary path grants:\n  r    /read\n  rxw  /tools\n  rw   /write"
	if context.notice != want {
		t.Errorf("got notice %q", context.notice)
	}
}

func TestRevokeCommandRemovesTheNamedPath(t *testing.T) {
	grants, current := fixturePathGrants()
	*current = []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	context, err := invokePathGrantCommand(t, grants, "/revoke /reference")
	if err != nil {
		t.Fatal(err)
	}
	if len(*current) != 0 {
		t.Errorf("got grants %#v", *current)
	}
	if len(context.events) != 1 || context.events[0].Name != "/reference" {
		t.Errorf("got events %#v", context.events)
	}
}
