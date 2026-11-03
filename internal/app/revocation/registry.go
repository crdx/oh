package revocation

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"crdx.org/oh/pkg/agent"
)

type Target struct {
	Key         string
	Description string
}

type Result struct {
	Events  []agent.Event
	Message string
}

type Source interface {
	Targets() []Target
	Revoke(key string) (Result, error)
}

type Resolver interface {
	Resolve(key string) (string, bool)
}

type Registry struct {
	mutex   sync.Mutex
	sources []Source
}

type route struct {
	source Source
	owner  int
}

type snapshot struct {
	targets []Target
	routes  map[string]route
	sources []Source
}

func (self *Registry) Register(source Source) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.sources = append(self.sources, source)
}

func (self *Registry) Targets() ([]Target, error) {
	state, err := self.snapshot()
	return state.targets, err
}

func (self *Registry) Revoke(key string) (Result, error) {
	state, err := self.snapshot()
	if err != nil {
		return Result{}, err
	}
	if route, exists := state.routes[key]; exists {
		return route.source.Revoke(key)
	}

	canonicalKey := ""
	for owner, source := range state.sources {
		resolver, canResolve := source.(Resolver)
		if !canResolve {
			continue
		}
		resolvedKey, isResolved := resolver.Resolve(key)
		if !isResolved {
			continue
		}
		route, exists := state.routes[resolvedKey]
		if !exists || route.owner != owner {
			continue
		}
		if canonicalKey != "" && canonicalKey != resolvedKey {
			return Result{}, fmt.Errorf("ambiguous revocation target %q", key)
		}
		canonicalKey = resolvedKey
	}
	if canonicalKey == "" {
		return Result{}, fmt.Errorf("no revocable target %q", key)
	}
	return state.routes[canonicalKey].source.Revoke(canonicalKey)
}

func (self *Registry) snapshot() (snapshot, error) {
	self.mutex.Lock()
	sources := slices.Clone(self.sources)
	self.mutex.Unlock()

	state := snapshot{sources: sources, routes: make(map[string]route)}
	for owner, source := range sources {
		if isNilSource(source) {
			return snapshot{}, errors.New("revocation source is nil")
		}
		for _, target := range source.Targets() {
			if target.Key == "" {
				return snapshot{}, errors.New("revocation target has no key")
			}
			if _, exists := state.routes[target.Key]; exists {
				return snapshot{}, fmt.Errorf("duplicate revocation target %q", target.Key)
			}
			state.targets = append(state.targets, target)
			state.routes[target.Key] = route{source: source, owner: owner}
		}
	}
	return state, nil
}

func isNilSource(source Source) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	nilKinds := []reflect.Kind{reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice}
	return slices.Contains(nilKinds, value.Kind()) && value.IsNil()
}
