package revocation

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

type fixtureSource struct {
	targets []Target
	aliases map[string]string
	calls   []string
	result  Result
	failure error
}

func (self *fixtureSource) Targets() []Target { return slices.Clone(self.targets) }

func (self *fixtureSource) Resolve(key string) (string, bool) {
	canonicalKey, exists := self.aliases[key]
	return canonicalKey, exists
}

func (self *fixtureSource) Revoke(key string) (Result, error) {
	self.calls = append(self.calls, key)
	return self.result, self.failure
}

func TestRegistryListsLiveTargetsAndPreservesRevocationResults(t *testing.T) {
	first := &fixtureSource{targets: []Target{{Key: "/path", Description: "temporary grant"}}}
	second := &fixtureSource{
		targets: []Target{{Key: "8080", Description: "forwarded port"}},
		result:  Result{Events: []agent.Event{{Name: "port closed"}}, Message: "Closed."},
	}
	registry := &Registry{}
	registry.Register(first)
	registry.Register(second)
	got, err := registry.Targets()
	if err != nil || !slices.Equal(got, append(slices.Clone(first.targets), second.targets...)) {
		t.Fatalf("got targets %v, %v", got, err)
	}
	result, err := registry.Revoke("8080")
	if err != nil || !reflect.DeepEqual(result, second.result) || len(first.calls) != 0 {
		t.Fatalf("revocation returned %+v, %v; other calls %v", result, err, first.calls)
	}
	second.targets = nil
	if _, err := registry.Revoke("8080"); err == nil || len(second.calls) != 1 {
		t.Fatalf("a removed target was reused: %v, %v", err, second.calls)
	}
}

func TestRegistryRefusesDuplicateKeysIncludingLaterCollisions(t *testing.T) {
	first := &fixtureSource{targets: []Target{{Key: "same"}}}
	second := &fixtureSource{}
	registry := &Registry{}
	registry.Register(first)
	registry.Register(second)
	if _, err := registry.Targets(); err != nil {
		t.Fatal(err)
	}
	second.targets = []Target{{Key: "same"}}
	if _, err := registry.Targets(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("got duplicate listing error %v", err)
	}
	if _, err := registry.Revoke("same"); err == nil || len(first.calls)+len(second.calls) != 0 {
		t.Fatalf("duplicate key invoked a source: %v", err)
	}
	second.targets = nil
	first.targets = append(first.targets, first.targets[0])
	if _, err := registry.Targets(); err == nil {
		t.Fatal("duplicates within one source were accepted")
	}
}

func TestRegistryRoutesAliasesOnlyToTheirOwnActiveTargets(t *testing.T) {
	first := &fixtureSource{
		targets: []Target{{Key: "/path"}},
		aliases: map[string]string{"relative": "/path", "foreign": "8080", "gone": "/gone", "8080": "/path"},
	}
	second := &fixtureSource{targets: []Target{{Key: "8080"}}}
	registry := &Registry{}
	registry.Register(first)
	registry.Register(second)
	for _, key := range []string{"relative", "8080"} {
		if _, err := registry.Revoke(key); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(first.calls, []string{"/path"}) || !slices.Equal(second.calls, []string{"8080"}) {
		t.Fatalf("wrong canonical routes: %v, %v", first.calls, second.calls)
	}
	for _, key := range []string{"foreign", "gone", "unknown"} {
		if _, err := registry.Revoke(key); err == nil {
			t.Errorf("accepted unowned alias %q", key)
		}
	}
	second.aliases = map[string]string{"relative": "8080"}
	if _, err := registry.Revoke("relative"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous alias returned %v", err)
	}
	if len(first.calls) != 1 || len(second.calls) != 1 {
		t.Fatal("an invalid alias revoked a target")
	}
}

func TestRegistryRejectsInvalidSourcesAndPropagatesSourceFailures(t *testing.T) {
	var missing *fixtureSource
	for _, source := range []Source{nil, missing, &fixtureSource{targets: []Target{{}}}} {
		registry := &Registry{}
		registry.Register(source)
		if _, err := registry.Targets(); err == nil {
			t.Fatal("invalid source was accepted")
		}
	}
	failure := errors.New("cannot revoke")
	registry := &Registry{}
	registry.Register(&fixtureSource{targets: []Target{{Key: "target"}}, failure: failure})
	if _, err := registry.Revoke("target"); !errors.Is(err, failure) {
		t.Fatalf("source failure became %v", err)
	}
}
