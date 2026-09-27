package commands

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/shell"
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

func TestGrantsCommandListsEffectivePermanentAndTemporaryAccess(t *testing.T) {
	grants, current := fixturePathGrants()
	grants.Permanent = []shell.ScopedPathGrant{
		{Path: "/read", Access: pathgrant.ReadAccess, Kind: shell.ConfiguredGrant},
		{Path: "/write", Access: pathgrant.ReadAccess | pathgrant.WriteAccess, Kind: shell.ConfiguredGrant},
	}
	*current = []pathgrant.Grant{
		{Path: "/read", Access: pathgrant.ReadAccess | pathgrant.ExecAccess},
		{Path: "/tools", Access: pathgrant.ReadAccess | pathgrant.WriteAccess | pathgrant.ExecAccess},
	}
	context, err := invokePathGrantCommand(t, grants, "/grants")
	if err != nil {
		t.Fatal(err)
	}
	want := "Paths:\n" +
		"  rw   config                /write\n" +
		"  rx   config + temporary    /read\n" +
		"  rxw  temporary             /tools"
	if context.notice != want {
		t.Errorf("got notice %q", context.notice)
	}
}

func TestGrantsCommandRejectsAnUnknownView(t *testing.T) {
	grants, _ := fixturePathGrants()
	_, err := invokePathGrantCommand(t, grants, "/grants verbose")
	if !slash.IsUsageError(err) {
		t.Errorf("got %v", err)
	}
}

func TestGrantsCommandReadsLiveWorkspaceAccess(t *testing.T) {
	grants, _ := fixturePathGrants()
	currentCaps := caps.Read
	grants.GetCurrentCaps = func() caps.Set { return currentCaps }
	grants.GetPermanent = func() []shell.ScopedPathGrant {
		access := shell.ReadAccess
		if currentCaps.Has(caps.Write) {
			access |= shell.WriteAccess
		}
		return []shell.ScopedPathGrant{{Path: "/workspace", Access: access, Kind: shell.WorkspaceGrant}}
	}

	context, err := invokePathGrantCommand(t, grants, "/grants")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(context.notice, "r    workspace") {
		t.Errorf("read-only notice %q", context.notice)
	}

	currentCaps |= caps.Write
	context, err = invokePathGrantCommand(t, grants, "/grants")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(context.notice, "rw   workspace") {
		t.Errorf("writable notice %q", context.notice)
	}
}

func TestPathGrantRowsGroupMatchingSourcesAndFoldSiblingPaths(t *testing.T) {
	grants := []effectivePathGrant{
		{path: "/etc/two", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.ConfiguredGrant}},
		{path: "/dev/null", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.ConfiguredGrant}},
		{path: "/etc/one", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.ConfiguredGrant}},
	}
	want := "Paths:\n  r    config                /dev/null /etc/{one,two}"
	if got := formatPathGrants(grants, false); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathGrantRowsSummariseEnabledSkills(t *testing.T) {
	grants := []effectivePathGrant{
		{path: "/skills/one", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.GlobalSkillGrant}},
		{path: "/skills/two", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.GlobalSkillGrant}},
	}
	want := "Paths:\n  r    skills                2 enabled"
	if got := formatPathGrants(grants, false); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	expandedWant := "Paths:\n  r    skills                /skills/{one,two}"
	if got := formatPathGrants(grants, true); got != expandedWant {
		t.Errorf("expanded got %q, want %q", got, expandedWant)
	}
}

func TestPathGrantRowsDoNotFoldNamesThatUseBraceSyntax(t *testing.T) {
	paths := compactPaths([]string{"/reference/a,b", "/reference/c"})
	want := []string{"/reference/a,b", "/reference/c"}
	if !slices.Equal(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
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
