package permission

import (
	"fmt"
	"slices"
	"sync"
	"time"
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

type Setting struct {
	Rule    Rule
	Timeout time.Duration
}

func (self Setting) IsAllowed() bool {
	return self.Rule == Allow
}

func (self Setting) TimeoutOr(fallback time.Duration) time.Duration {
	if self.Timeout > 0 {
		return self.Timeout
	}
	return fallback
}

type Set struct {
	Network Setting
	Lookup  Setting
	Fetch   Setting
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

func (self *Live) Network() Setting {
	return self.Get().Network
}

func (self *Live) Lookup() Setting {
	return self.Get().Lookup
}

func (self *Live) Fetch() Setting {
	return self.Get().Fetch
}
