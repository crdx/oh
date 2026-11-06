package commands

import (
	"strconv"

	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/revocation"
	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/pkg/agent"
)

type pathRevocations struct {
	grants PathGrants
}

func (self pathRevocations) Targets() []revocation.Target {
	var targets []revocation.Target
	for _, grant := range self.grants.GetCurrent() {
		targets = append(targets, revocation.Target{Key: grant.Path, Description: tr.PathGrantTarget})
	}
	return targets
}

func (self pathRevocations) Resolve(key string) (string, bool) {
	if self.grants.Resolve == nil {
		return "", false
	}
	canonicalKey, err := self.grants.Resolve(key)
	return canonicalKey, err == nil
}

func (self pathRevocations) Revoke(key string) (revocation.Result, error) {
	event, err := self.grants.Revoke(key)
	if err != nil {
		return revocation.Result{}, err
	}
	return revocation.Result{Events: []agent.Event{event}}, nil
}

type portRevocations struct {
	forwards Forwards
}

func (self portRevocations) Targets() []revocation.Target {
	var targets []revocation.Target
	for _, port := range self.forwards.GetCurrent() {
		targets = append(targets, revocation.Target{Key: strconv.Itoa(int(port)), Description: tr.ForwardedPortTarget})
	}
	return targets
}

func (self portRevocations) Resolve(key string) (string, bool) {
	port, err := portgrant.ParsePort(key)
	if err != nil {
		return "", false
	}
	return strconv.Itoa(int(port)), true
}

func (self portRevocations) Revoke(key string) (revocation.Result, error) {
	port, err := portgrant.ParsePort(key)
	if err != nil {
		return revocation.Result{}, err
	}
	event, err := self.forwards.Revoke(port)
	if err != nil {
		return revocation.Result{}, err
	}
	return revocation.Result{Events: []agent.Event{event}}, nil
}

func newRevocations(grants PathGrants, forwards Forwards, sources []revocation.Source) *revocation.Registry {
	registry := &revocation.Registry{}
	if grants.isConfigured() {
		registry.Register(pathRevocations{grants: grants})
	}
	if forwards.isConfigured() {
		registry.Register(portRevocations{forwards: forwards})
	}
	for _, source := range sources {
		registry.Register(source)
	}
	return registry
}
