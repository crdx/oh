package commands

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/revocation"
	"crdx.org/oh/pkg/agent"
)

type extraRevocations struct {
	targets []revocation.Target
	keys    []string
}

func (self *extraRevocations) Targets() []revocation.Target { return slices.Clone(self.targets) }

func (self *extraRevocations) Revoke(key string) (revocation.Result, error) {
	self.keys = append(self.keys, key)
	self.targets = nil
	return revocation.Result{Events: []agent.Event{{Name: key}}, Message: "Removed."}, nil
}

func TestRevokeAcceptsRegisteredSourcesWithoutPathsOrPorts(t *testing.T) {
	source := &extraRevocations{targets: []revocation.Target{{Key: "sample", Description: "another kind"}}}
	registry := newCommandRegistry(t, commandEnvironment{revocations: []revocation.Source{source}})
	invocation, exists := registry.Find("/revoke sample")
	if !exists {
		t.Fatal("revoke was not registered")
	}
	context := newCommandTestContext(t)
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(source.keys, []string{"sample"}) || len(context.events) != 1 || context.success != "Removed." {
		t.Fatalf("source result was lost: %v, %+v", source.keys, context)
	}
	if err := invocation.Command.Run(context, invocation.Arguments); err == nil || len(source.keys) != 1 {
		t.Fatal("a target that disappeared was revoked again")
	}
}

func TestRevocationAdaptersNormaliseAliasesBeforeCallingTheirOwners(t *testing.T) {
	grants, paths := fixturePathGrants()
	*paths = []pathgrant.Grant{{Path: "/canonical", Access: pathgrant.ReadAccess}}
	grants.Resolve = func(path string) (string, error) { return "/canonical", nil }
	ports, forwarded := fixturePortGrants()
	*forwarded = []uint16{8080}
	registry := newRevocations(grants, ports, nil)
	for _, key := range []string{"relative", "08080"} {
		if _, err := registry.Revoke(key); err != nil {
			t.Fatal(err)
		}
	}
	if len(*paths) != 0 || len(*forwarded) != 0 {
		t.Fatalf("aliases did not revoke their targets: %v, %v", *paths, *forwarded)
	}
}
