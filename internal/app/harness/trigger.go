package harness

import (
	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/input"
	"crdx.org/oh/internal/app/key"
)

func (self *App) triggerChanges() <-chan struct{} {
	if self.completer == nil {
		return nil
	}

	return self.completer.Changes()
}

func (self *App) receiveTriggerChange() {
	if self.completer != nil {
		self.completer.Receive()
	}
}

func (self *App) applyToCompleter(inputLine *edit.Input, keypress key.Key) bool {
	if self.completer == nil || inputLine.IsPasting() || inputLine.IsPrefixPending() {
		return false
	}

	if !self.completer.IsOpen() {
		if keypress != tabKey {
			return false
		}
		if self.completer.Open(inputLine) {
			return true
		}
		invocation, isCommand := self.commands.Find(inputLine.Text())
		return isCommand && invocation.Arguments.Text == ""
	}
	if dismissesFeedback(keypress) && self.feedback.CanBeDismissed() {
		return false
	}

	return self.completer.Apply(inputLine, keypress)
}

func (self *App) completeTyped(inputLine *edit.Input, keypress key.Key) {
	if self.completer != nil && !inputLine.IsPasting() {
		self.completer.Typed(inputLine, keypress)
	}
}

var tabKey = key.Key{Code: key.Rune, Value: '\t'}

func (self *App) syncCompleter(inputLine *edit.Input) {
	if self.completer != nil {
		self.completer.Sync(inputLine)
	}
}

func (self *App) dropdownRows(block input.Block, columns int) []string {
	if self.completer == nil || !self.completer.IsOpen() {
		return nil
	}

	room, isBounded := self.screen.FooterRoom()
	if !isBounded {
		return self.completer.Rows(columns, dropdown.MaxRows)
	}

	rows, _, _ := block.Rows(columns)

	return self.completer.Rows(columns, room-len(rows))
}
