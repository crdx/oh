package permission

import (
	"fmt"
	"slices"
	"sync"
)

type Rule string

const (
	Ask   Rule = "ask"
	Allow Rule = "allow"
)

var rules = []Rule{Ask, Allow}

func ParseRule(value string) (Rule, error) {
	if slices.Contains(rules, Rule(value)) {
		return Rule(value), nil
	}

	return "", fmt.Errorf("must be %q or %q, got %q", Ask, Allow, value)
}

type Set struct {
	Network Rule
	Lookup  Rule
	Fetch   Rule
}

type Live struct {
	mutex sync.RWMutex
	set   Set
}

func New(set Set) *Live {
	return &Live{set: set}
}

func (self *Live) Replace(set Set) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.set = set
}

func (self *Live) Get() Set {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	return self.set
}

func (self *Live) Network() Rule {
	return self.Get().Network
}

func (self *Live) Lookup() Rule {
	return self.Get().Lookup
}

func (self *Live) Fetch() Rule {
	return self.Get().Fetch
}
