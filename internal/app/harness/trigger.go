package harness

import (
	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/input"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/pkg/agent"
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
	if self.expandSnippetInline(inputLine, keypress) {
		return true
	}

	if !self.completer.IsOpen() {
		if keypress != tabKey {
			return false
		}
		if self.completer.Open(inputLine) {
			return true
		}
		_, isCommand := self.commands.CommandName(inputLine.Text())
		return isCommand
	}
	if dismissesFeedback(keypress) && self.feedback.CanBeDismissed() {
		return false
	}

	return self.completer.Apply(inputLine, keypress)
}

func (self *App) expandSnippetInline(inputLine *edit.Input, keypress key.Key) bool {
	runes := inputLine.Runes()
	if keypress != tabKey || inputLine.Cursor() != len(runes) || len(runes) < 2 || runes[0] != '/' || runes[1] != '/' {
		return false
	}

	invocation, isFound := self.commands.Find(inputLine.Text())
	if !isFound {
		return false
	}
	expansion, isExpandable, err := invocation.Expand()
	if !isExpandable {
		return false
	}
	if err != nil {
		self.showFeedback(feedback.Command, feedback.Message{
			Text:   slash.FormatError(invocation, err),
			Status: agent.ErrorStatus,
		})
		return true
	}

	inputLine.Replace(0, len(runes), expansion)
	self.completer.Close()
	return true
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
