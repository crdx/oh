package harness

import (
	"fmt"
	"strings"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const (
	shortcutKeyWidth = 20
	shortcutPinRows  = 3
)

type shortcutHelpState struct {
	isOpen bool
	scroll int
}

type keyboardShortcut struct {
	keys   string
	action string
}

func isShortcutHelpKey(keypress key.Key) bool {
	if keypress.Code != key.Rune || !keypress.Mod.Has(key.Ctrl) || keypress.Mod.Has(key.Alt) {
		return false
	}
	return keypress.Value == '/' || keypress.Value == '?' || keypress.Value == '_'
}

func (self *App) handleShortcutHelp(inputLine *edit.Input, keypress key.Key) bool {
	if inputLine.IsPasting() {
		return false
	}
	if isShortcutHelpKey(keypress) {
		inputLine.BreakKeySequence()
		self.applyUndo(inputLine, keypress)
		self.shortcutHelp.isOpen = !self.shortcutHelp.isOpen
		self.shortcutHelp.scroll = len(self.shortcutRows(inputLine, self.screen.Columns()))
		return true
	}
	if !self.shortcutHelp.isOpen {
		return false
	}
	page, _ := self.screen.FooterRoom()
	page = max(1, page-shortcutPinRows*2)
	switch keypress.Code {
	case key.Up:
		self.shortcutHelp.scroll++
	case key.Down:
		self.shortcutHelp.scroll--
	case key.PageUp:
		self.shortcutHelp.scroll += page
	case key.PageDown:
		self.shortcutHelp.scroll -= page
	case key.Home:
		self.shortcutHelp.scroll = len(self.shortcutRows(inputLine, self.screen.Columns()))
	case key.End:
		self.shortcutHelp.scroll = 0
	case key.Escape:
		self.shortcutHelp.isOpen = false
	case key.Rune:
		if isControlKey(keypress, 'c') {
			self.shortcutHelp.isOpen = false
		}
	case key.Enter, key.Backspace, key.Delete, key.Left, key.Right, key.PasteStart, key.PasteEnd,
		key.Clipboard, key.FocusIn, key.FocusOut, key.Unknown:
	}
	return true
}

func (self *App) showShortcutHelp(inputLine *edit.Input) {
	rows := self.shortcutRows(inputLine, self.screen.Columns())
	self.shortcutHelp.scroll = self.screen.InertFooter(rows, len(rows)-2, output.Pins{
		Head: shortcutPinRows,
		Tail: shortcutPinRows,
	}, self.shortcutHelp.scroll)
}

func (self *App) shortcutRows(inputLine *edit.Input, columns int) []string {
	rule := self.ruleStyle()(strings.Repeat("─", max(0, columns)))
	rows := []string{rule, style.Heading(width.Elide("Keyboard shortcuts", columns)), ""}
	for _, shortcut := range self.keyboardShortcuts(inputLine) {
		if shortcut.keys == "" {
			rows = append(rows, "", style.Subtle(shortcut.action))
			continue
		}
		if columns < shortcutKeyWidth*2 {
			rows = append(rows, style.Accent(width.Elide(shortcut.keys, columns)))
			rows = append(rows, width.WrapIndented("  "+shortcut.action, columns, 2)...)
			continue
		}
		line := style.Accent(fmt.Sprintf("%-*s", shortcutKeyWidth, shortcut.keys)) + shortcut.action
		rows = append(rows, width.WrapIndented(line, columns, shortcutKeyWidth)...)
	}
	return append(rows, "", style.Subtle(width.Elide("^/ or ^C closes · ↑/↓ scrolls", columns)), rule)
}

func (self *App) keyboardShortcuts(inputLine *edit.Input) []keyboardShortcut {
	shortcuts := []keyboardShortcut{
		{keys: "^/ or ^?", action: "Show or close this reference"},
	}
	if self.isAwaitingAnswer() {
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "Escape / ^C", action: "Cancel the question"},
			keyboardShortcut{keys: "Enter", action: "Choose the selected answer"},
			keyboardShortcut{keys: "Left / Right / Tab", action: "Select an answer"},
			keyboardShortcut{keys: "Up / Down", action: "Scroll question details"},
		)
		for _, option := range self.question.request.Question.Options {
			if option.Key != 0 {
				shortcuts = append(shortcuts, keyboardShortcut{keys: string(option.Key), action: option.Label})
			}
		}
		return shortcuts
	}
	if self.isEditorAwaited() {
		return append(shortcuts, keyboardShortcut{keys: "Escape / ^C / ^D", action: "Cancel external editing"})
	}
	if inputLine.IsSearching() {
		return append(shortcuts,
			keyboardShortcut{keys: "Escape", action: "Leave the search"},
			keyboardShortcut{keys: "^R", action: "Find the previous match"},
			keyboardShortcut{keys: "^C twice", action: "Clear search and draft"},
			keyboardShortcut{keys: "Backspace", action: "Shorten the search"},
		)
	}
	if inputLine.IsPrefixPending() {
		return append(shortcuts, self.modeShortcuts()...)
	}
	if self.currentTurn.Running() || self.isHostCommandUnderway() {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "Escape / ^D", action: "Stop the foreground command, then the turn"})
	} else {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "^D", action: "Quit from an empty draft"})
	}
	shortcuts = append(shortcuts,
		keyboardShortcut{keys: "^C twice / ^U", action: "Clear the draft without stopping work"},
		keyboardShortcut{keys: "Shift+Enter / ^J", action: "Insert a newline"},
	)
	if self.currentTurn.Running() && !self.currentTurn.Cancelled() {
		enterAction, alternateAction := "Queue the draft", "Stop the turn and send the draft"
		if self.experimental.IsEnabled(experimental.EnterInterrupts) {
			enterAction, alternateAction = alternateAction, enterAction
		}
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "Enter", action: enterAction},
			keyboardShortcut{keys: "Alt+Enter", action: alternateAction},
		)
	} else {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "Enter / Alt+Enter", action: "Send the draft"})
	}
	if self.currentTurn.HasInterjections() {
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "^Z / Escape / ^D", action: "Take back the newest queued message"},
			keyboardShortcut{keys: "Enter twice", action: "Send the queued messages"},
		)
	} else {
		if self.undoBlocker() == "" {
			shortcuts = append(shortcuts, keyboardShortcut{keys: "^Z", action: "Take back the last turn"})
		}
		if !self.currentTurn.Running() {
			shortcuts = append(shortcuts, keyboardShortcut{keys: "Enter twice", action: "Continue or send pending notices"})
		}
	}
	shortcuts = append(shortcuts,
		keyboardShortcut{action: "Editing"},
		keyboardShortcut{keys: "Tab", action: "Complete a word or choose a suggestion"},
		keyboardShortcut{keys: "Up / Down", action: "Move by line or recall history"},
		keyboardShortcut{keys: "^R", action: "Search input history"},
		keyboardShortcut{keys: "Home / ^A", action: "Move to the start of the line"},
		keyboardShortcut{keys: "End / ^E", action: "Move to the end of the line"},
		keyboardShortcut{keys: "^Left / ^Right", action: "Move by word"},
		keyboardShortcut{keys: "^Backspace / ^W", action: "Delete the previous word"},
		keyboardShortcut{keys: "^Delete", action: "Delete the next word"},
		keyboardShortcut{keys: "^K", action: "Delete to the end of the line"},
		keyboardShortcut{keys: "^G", action: "Edit the draft externally"},
		keyboardShortcut{keys: "^S", action: "Stash the draft or restore the latest stash"},
		keyboardShortcut{keys: "^Shift+S", action: "Swap the draft with the latest stash"},
		keyboardShortcut{keys: "^L", action: "Redraw the conversation"},
	)
	if _, hasAnswer := self.getLastMessage(); hasAnswer {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "^.", action: "Reply to the last answer in the editor"})
	}
	if self.mode != nil {
		shortcuts = append(shortcuts, keyboardShortcut{action: "Access: press ^X, then a letter"})
		shortcuts = append(shortcuts, self.modeShortcuts()...)
	}
	return shortcuts
}

func (self *App) modeShortcuts() []keyboardShortcut {
	shortcuts := []keyboardShortcut{{keys: "Escape / ^C", action: "Cancel the mode prefix"}}
	if self.mode == nil {
		return shortcuts
	}
	if !self.mode.IsUnconfined() {
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "w", action: "Toggle workspace writes"},
			keyboardShortcut{keys: "x", action: "Toggle shell execution"},
			keyboardShortcut{keys: "n", action: "Toggle host network"},
			keyboardShortcut{keys: "g", action: "Toggle repository history"},
		)
	}
	shortcuts = append(shortcuts,
		keyboardShortcut{keys: "s", action: "Toggle subagents"},
		keyboardShortcut{keys: "l", action: "Toggle lookup"},
	)
	for _, flag := range self.mode.Groups().Flags {
		shortcuts = append(shortcuts, keyboardShortcut{keys: string(flag), action: "Toggle this custom tool group"})
	}
	return shortcuts
}
