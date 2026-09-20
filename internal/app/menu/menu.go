package menu

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tty"
)

const (
	clearLine               = ansi.EraseRow
	hideCursor              = ansi.HideCursor
	showCursor              = ansi.ShowCursor
	restoreMenuPresentation = ansi.Reset + showCursor
)

func ChooseIndex(terminal *os.File, output io.Writer, prompt string, labels []string) (int, error) {
	if terminal == nil || !term.IsTerminal(int(terminal.Fd())) {
		return 0, errors.New("menu needs an interactive terminal")
	}

	restore, err := tty.Raw(terminal, output)
	if err != nil {
		return 0, err
	}
	defer restore()

	restorePresentation, err := beginMenuPresentation(output)
	if err != nil {
		return 0, err
	}
	defer restorePresentation()

	_, height, err := term.GetSize(int(terminal.Fd()))
	if err != nil {
		height = 24
	}

	menu := menu{
		labels: labels,
		rows:   min(max(height-4, 1), len(labels)),
	}
	if _, err := io.WriteString(output, RenderMenu(prompt, labels, 0)); err != nil {
		return 0, err
	}

	decoder := key.NewTerminalDecoder(bufio.NewReader(terminal), terminal)
	for {
		keypress, err := decoder.Next()
		if err != nil {
			return 0, err
		}

		switch keypress.Code {
		case key.Up:
			menu.move(-1)
		case key.Down:
			menu.move(1)
		case key.Home:
			menu.cursor = 0
			menu.scroll()
		case key.End:
			menu.cursor = len(labels) - 1
			menu.scroll()
		case key.Enter:
			return menu.cursor, nil
		case key.Escape:
			return 0, ErrCancelled
		case key.Rune:
			if keypress.Value == 'q' || keypress.Value == 'c' && keypress.Mod.Has(key.Ctrl) {
				return 0, ErrCancelled
			}
			continue
		case key.Backspace, key.Delete, key.Left, key.Right, key.PageUp, key.PageDown,
			key.PasteStart, key.PasteEnd, key.Clipboard, key.FocusIn, key.FocusOut, key.Unknown:
			continue
		default:
			continue
		}

		if _, err := io.WriteString(output, ansi.Up(menu.rows)+menu.render(true)); err != nil {
			return 0, err
		}
	}
}

func beginMenuPresentation(output io.Writer) (func(), error) {
	if _, err := io.WriteString(output, hideCursor); err != nil {
		return nil, err
	}
	return func() { _, _ = io.WriteString(output, restoreMenuPresentation) }, nil
}

type menu struct {
	labels []string
	cursor int
	offset int
	rows   int
}

func RenderMenu(prompt string, labels []string, cursor int) string {
	displayedMenu := menu{labels: labels, cursor: cursor, rows: len(labels)}
	return fmt.Sprintf("%s\r\n\r\n%s", prompt, displayedMenu.render(false))
}

func (self *menu) move(distance int) {
	self.cursor = min(max(self.cursor+distance, 0), len(self.labels)-1)
	self.scroll()
}

func (self *menu) scroll() {
	switch {
	case self.cursor < self.offset:
		self.offset = self.cursor
	case self.cursor >= self.offset+self.rows:
		self.offset = self.cursor - self.rows + 1
	}
}

func (self *menu) render(shouldClear bool) string {
	var renderedText strings.Builder
	for i := self.offset; i < self.offset+self.rows; i++ {
		line := "  " + self.labels[i]
		if i == self.cursor {
			line = style.ChosenRow.Over("› " + self.labels[i])
		}
		if shouldClear {
			renderedText.WriteString(clearLine)
		}
		renderedText.WriteString(line)
		renderedText.WriteString("\r\n")
	}
	return renderedText.String()
}
