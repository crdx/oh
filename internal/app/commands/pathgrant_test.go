package commands

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
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
	context := newCommandTestContext(t)
	return context, invocation.Command.Run(context, invocation.Arguments)
}

func TestGrantCommandPreservesSpacesInAQuotedPath(t *testing.T) {
	grants, current := fixturePathGrants()
	context, err := invokePathGrantCommand(t, grants, `/grant rw "/reference/path with spaces"`)
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

func TestGrantCommandGrantsEveryPathOnce(t *testing.T) {
	grants, current := fixturePathGrants()
	context, err := invokePathGrantCommand(t, grants, `/grant r /one "/two words" /one ~/three`)
	if err != nil {
		t.Fatal(err)
	}
	want := []pathgrant.Grant{
		{Path: "/one", Access: pathgrant.ReadAccess},
		{Path: "/two words", Access: pathgrant.ReadAccess},
		{Path: "~/three", Access: pathgrant.ReadAccess},
	}
	if !slices.Equal(*current, want) {
		t.Errorf("got grants %#v", *current)
	}
	if len(context.events) != len(want) {
		t.Errorf("got events %#v", context.events)
	}
}

func TestGrantCommandGrantsWhatItCanAndNamesEveryFailure(t *testing.T) {
	grants, current := fixturePathGrants()
	grant := grants.Grant
	grants.Grant = func(path string, access pathgrant.Access) (agent.Event, error) {
		if strings.HasPrefix(path, "/missing") {
			return agent.Event{}, errors.New("could not resolve " + path)
		}
		return grant(path, access)
	}
	context, err := invokePathGrantCommand(t, grants, "/grant r /missing/one /present /missing/two")
	if err == nil || err.Error() != "could not resolve /missing/one; could not resolve /missing/two" {
		t.Errorf("got error %v", err)
	}
	want := []pathgrant.Grant{{Path: "/present", Access: pathgrant.ReadAccess}}
	if !slices.Equal(*current, want) {
		t.Errorf("got grants %#v", *current)
	}
	if len(context.events) != 1 {
		t.Errorf("got events %#v", context.events)
	}
}

func TestGrantCommandRefusesAnUnclosedQuote(t *testing.T) {
	grants, current := fixturePathGrants()
	_, err := invokePathGrantCommand(t, grants, `/grant r /one "/two words`)
	if !slash.IsUsageError(err) {
		t.Errorf("got %v", err)
	}
	if len(*current) != 0 {
		t.Errorf("got grants %#v", *current)
	}
}

func TestGrantCommandRefusesNoPath(t *testing.T) {
	grants, _ := fixturePathGrants()
	for _, input := range []string{"/grant r", "/grant r   "} {
		if _, err := invokePathGrantCommand(t, grants, input); !slash.IsUsageError(err) {
			t.Errorf("%q gave %v", input, err)
		}
	}
}

func TestGrantCommandRejectsExecutableAccess(t *testing.T) {
	grants, _ := fixturePathGrants()
	_, err := invokePathGrantCommand(t, grants, "/grant rx /reference")
	if !slash.IsUsageError(err) {
		t.Errorf("got %v", err)
	}
}

func TestGrantPathCompletionAcceptsReadAndReadWriteAccess(t *testing.T) {
	grants, _ := fixturePathGrants()
	registry := newCommandRegistry(t, commandEnvironment{pathGrants: grants})
	source := slash.NewPathSource(t.TempDir(), func() slash.Registry { return registry })

	for _, access := range []string{"r", "rw"} {
		text := "/grant " + access + " ~/reference"
		if _, found := source.Find([]rune(text), len([]rune(text))); !found {
			t.Errorf("did not find the path after %q", access)
		}
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
		{Path: "/read", Access: pathgrant.ReadAccess},
		{Path: "/tools", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
	}
	context, err := invokePathGrantCommand(t, grants, "/grants")
	if err != nil {
		t.Fatal(err)
	}
	want := "Paths:\n" +
		"  rw   config                /write\n" +
		"  rx   config + temporary    /read\n" +
		"  rxw  temporary             /tools"
	if got := style.Plain(context.notice); got != want {
		t.Errorf("got notice %q", got)
	}
}

func TestGrantsCommandRejectsAnUnknownView(t *testing.T) {
	grants, _ := fixturePathGrants()
	_, err := invokePathGrantCommand(t, grants, "/grants verbose")
	if !slash.IsUsageError(err) {
		t.Errorf("got %v", err)
	}
}

func TestGrantsAllUsesTheSameContentWithoutElision(t *testing.T) {
	grants, _ := fixturePathGrants()
	grants.Permanent = []shell.ScopedPathGrant{
		{Path: "/skills/one", Access: pathgrant.ReadAccess, Kind: shell.GlobalSkillGrant},
		{Path: "/skills/two", Access: pathgrant.ReadAccess, Kind: shell.GlobalSkillGrant},
	}

	standard, err := invokePathGrantCommand(t, grants, "/grants")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := invokePathGrantCommand(t, grants, "/grants all")
	if err != nil {
		t.Fatal(err)
	}
	if standard.notice != explicit.notice {
		t.Errorf("/grants gave %q, /grants all gave %q", standard.notice, explicit.notice)
	}
	if !standard.isListing {
		t.Error("/grants did not request one-line rows")
	}
	if explicit.isListing || explicit.continuationIndent != pathGrantContinuationIndent {
		t.Errorf("/grants all requested listing %t with continuation indent %d", explicit.isListing, explicit.continuationIndent)
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
	if got := style.Plain(formatPathGrants(grants)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathGrantRowsListEnabledSkillPaths(t *testing.T) {
	grants := []effectivePathGrant{
		{path: "/skills/one", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.GlobalSkillGrant}},
		{path: "/skills/two", access: pathgrant.ReadAccess, sources: []shell.GrantKind{shell.GlobalSkillGrant}},
	}
	want := "Paths:\n  r    skills                /skills/{one,two}"
	if got := style.Plain(formatPathGrants(grants)); got != want {
		t.Errorf("got %q, want %q", got, want)
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
