package terminal

import (
	"sync/atomic"

	"crdx.org/oh/internal/app/key"
)

type focus struct {
	isFocused atomic.Bool
}

func newFocus() *focus {
	return &focus{}
}

func (self *Terminal) beginFocus() func() {
	if self.focus == nil {
		return func() {}
	}

	self.focus.isFocused.Store(true)
	return func() { self.focus.isFocused.Store(false) }
}

func (self *Terminal) ObserveFocus(code key.Code) bool {
	if self.focus == nil {
		return false
	}
	if code == key.FocusIn {
		self.focus.isFocused.Store(true)
		return true
	}
	if code == key.FocusOut {
		self.focus.isFocused.Store(false)
		return true
	}

	return false
}

func (self *Terminal) IsFocused() bool {
	return self.focus != nil && self.focus.isFocused.Load()
}
