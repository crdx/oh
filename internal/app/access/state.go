package access

import (
	"strings"
	"sync"
)

type Definition[Value any] struct {
	Clone    func(Value) Value
	Describe func(knownValue Value, currentValue Value) string
}

type State[Value any] struct {
	definition Definition[Value]

	mutex        sync.Mutex
	currentValue Value
	knownValue   Value
}

func New[Value any](currentValue Value, definition Definition[Value]) *State[Value] {
	return NewRestored(currentValue, currentValue, definition)
}

func NewRestored[Value any](currentValue Value, knownValue Value, definition Definition[Value]) *State[Value] {
	return &State[Value]{
		definition:   definition,
		currentValue: definition.Clone(currentValue),
		knownValue:   definition.Clone(knownValue),
	}
}

func (self *State[Value]) GetCurrent() Value {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.definition.Clone(self.currentValue)
}

func (self *State[Value]) GetKnown() Value {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.definition.Clone(self.knownValue)
}

func (self *State[Value]) Change(transform func(Value) Value) Value {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.definition.Clone(self.currentValue)
	self.currentValue = self.definition.Clone(transform(current))
	return self.definition.Clone(self.currentValue)
}

func (self *State[Value]) Replace(currentValue Value) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.currentValue = self.definition.Clone(currentValue)
}

func (self *State[Value]) Peek() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.definition.Describe(
		self.definition.Clone(self.knownValue),
		self.definition.Clone(self.currentValue),
	)
}

func (self *State[Value]) Inject() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	message := self.definition.Describe(
		self.definition.Clone(self.knownValue),
		self.definition.Clone(self.currentValue),
	)
	self.knownValue = self.definition.Clone(self.currentValue)
	return message
}

type Teller interface {
	Peek() string
	Inject() string
}

type Group struct {
	tellers []Teller
}

func NewGroup(tellers ...Teller) Group {
	return Group{tellers: append([]Teller(nil), tellers...)}
}

func (self Group) Peek() string {
	var messages []string
	for _, teller := range self.tellers {
		if message := teller.Peek(); message != "" {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, " ")
}

func (self Group) Inject() string {
	var messages []string
	for _, teller := range self.tellers {
		if message := teller.Inject(); message != "" {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, " ")
}
