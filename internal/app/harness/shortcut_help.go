package harness

import (
	"fmt"
	"strings"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tr"
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
	rows := []string{rule, style.Heading(width.Elide(tr.ShortcutHelpTitle, columns)), ""}
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
	return append(rows, "", style.Subtle(width.Elide(fmt.Sprintf(tr.ShortcutHelpFooter, "^/ or ^C", "↑/↓"), columns)), rule)
}

func (self *App) keyboardShortcuts(inputLine *edit.Input) []keyboardShortcut {
	shortcuts := []keyboardShortcut{
		{keys: "^/ or ^?", action: tr.ShowOrCloseShortcuts},
	}
	if self.isAwaitingAnswer() {
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "Escape / ^C", action: tr.CancelQuestion},
			keyboardShortcut{keys: "Enter", action: tr.ChooseSelectedAnswer},
			keyboardShortcut{keys: "Left / Right / Tab", action: tr.SelectAnswer},
			keyboardShortcut{keys: "Up / Down", action: tr.ScrollQuestionDetails},
		)
		for _, option := range self.question.request.Question.Options {
			if option.Key != 0 {
				shortcuts = append(shortcuts, keyboardShortcut{keys: string(option.Key), action: option.Label})
			}
		}
		return shortcuts
	}
	if self.isEditorAwaited() {
		return append(shortcuts, keyboardShortcut{keys: "Escape / ^C / ^D", action: tr.CancelExternalEditing})
	}
	if inputLine.IsSearching() {
		return append(shortcuts,
			keyboardShortcut{keys: "Escape", action: tr.LeaveSearch},
			keyboardShortcut{keys: "^R", action: tr.FindPreviousMatch},
			keyboardShortcut{keys: "^C twice", action: tr.ClearSearchAndDraft},
			keyboardShortcut{keys: "Backspace", action: tr.ShortenSearch},
		)
	}
	if inputLine.IsPrefixPending() {
		return append(shortcuts, self.modeShortcuts()...)
	}
	if self.currentTurn.Running() || self.isHostCommandUnderway() {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "Escape / ^D", action: tr.StopCommandThenTurn})
	} else {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "^D", action: tr.QuitFromEmptyDraft})
	}
	shortcuts = append(shortcuts,
		keyboardShortcut{keys: "^C twice / ^U", action: tr.ClearDraft},
		keyboardShortcut{keys: "Shift+Enter / ^J", action: tr.InsertNewline},
	)
	if self.currentTurn.Running() && !self.currentTurn.Cancelled() {
		enterAction, alternateAction := tr.QueueDraft, tr.StopTurnAndSendDraft
		if self.experimental.IsEnabled(experimental.EnterInterrupts) {
			enterAction, alternateAction = alternateAction, enterAction
		}
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "Enter", action: enterAction},
			keyboardShortcut{keys: "Alt+Enter", action: alternateAction},
		)
	} else {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "Enter / Alt+Enter", action: tr.SendDraft})
	}
	if self.currentTurn.HasInterjections() {
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "^Z / Escape / ^D", action: tr.TakeBackQueuedMessage},
			keyboardShortcut{keys: "Enter twice", action: tr.SendQueuedMessages},
		)
	} else {
		if self.undoBlocker() == "" {
			shortcuts = append(shortcuts, keyboardShortcut{keys: "^Z", action: tr.TakeBackLastTurn})
		}
		if !self.currentTurn.Running() {
			shortcuts = append(shortcuts, keyboardShortcut{keys: "Enter twice", action: tr.ContinueOrSendNotices})
		}
	}
	shortcuts = append(shortcuts,
		keyboardShortcut{action: tr.ShortcutEditingSection},
		keyboardShortcut{keys: "Tab", action: tr.CompleteWord},
		keyboardShortcut{keys: "Up / Down", action: tr.MoveByLine},
		keyboardShortcut{keys: "^R", action: tr.SearchInputHistory},
		keyboardShortcut{keys: "Home / ^A", action: tr.MoveToLineStart},
		keyboardShortcut{keys: "End / ^E", action: tr.MoveToLineEnd},
		keyboardShortcut{keys: "^Left / ^Right", action: tr.MoveByWord},
		keyboardShortcut{keys: "^Backspace / ^W", action: tr.DeletePreviousWord},
		keyboardShortcut{keys: "^Delete", action: tr.DeleteNextWord},
		keyboardShortcut{keys: "^K", action: tr.DeleteToLineEnd},
		keyboardShortcut{keys: "^G", action: tr.EditDraftExternally},
		keyboardShortcut{keys: "^S", action: tr.StashDraft},
		keyboardShortcut{keys: "^Shift+S", action: tr.SwapDraftWithStash},
		keyboardShortcut{keys: "^L", action: tr.RedrawConversation},
	)
	if _, hasAnswer := self.getLastMessage(); hasAnswer {
		shortcuts = append(shortcuts, keyboardShortcut{keys: "^.", action: tr.ReplyInEditor})
	}
	if self.mode != nil {
		shortcuts = append(shortcuts, keyboardShortcut{action: fmt.Sprintf(tr.ShortcutAccessSection, "^X")})
		shortcuts = append(shortcuts, self.modeShortcuts()...)
	}
	return shortcuts
}

func (self *App) modeShortcuts() []keyboardShortcut {
	shortcuts := []keyboardShortcut{{keys: "Escape / ^C", action: tr.CancelModePrefix}}
	if self.mode == nil {
		return shortcuts
	}
	if !self.mode.IsUnconfined() {
		shortcuts = append(shortcuts,
			keyboardShortcut{keys: "w", action: tr.ToggleWorkspaceWrites},
			keyboardShortcut{keys: "x", action: tr.ToggleShellExecution},
			keyboardShortcut{keys: "n", action: tr.ToggleHostNetwork},
			keyboardShortcut{keys: "g", action: tr.ToggleRepositoryHistory},
		)
	}
	shortcuts = append(shortcuts,
		keyboardShortcut{keys: "s", action: tr.ToggleSubagents},
		keyboardShortcut{keys: "l", action: tr.ToggleLookup},
	)
	for _, flag := range self.mode.Groups().Flags {
		shortcuts = append(shortcuts, keyboardShortcut{keys: string(flag), action: tr.ToggleCustomToolGroup})
	}
	return shortcuts
}
