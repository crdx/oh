package textsizing

import (
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/term"

	"crdx.org/oh/internal/app/tty"
)

const (
	beginProbe = "\x1b[?1049h\x1b[?25l\r\x1b[6n\x1b]66;w=2; \a\x1b[6n\x1b]66;s=2; \a\x1b[6n"
	endProbe   = "\x1b[?25h\x1b[?1049l"

	positionReplies = 3
)

var positionReply = regexp.MustCompile(`\x1b\[[0-9]+;[0-9]+R`)

func Detect(input *os.File, output *os.File) bool {
	if os.Getenv("KITTY_WINDOW_ID") == "" || !tty.Is(input) || !tty.Is(output) {
		return false
	}

	terminalState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return false
	}
	defer func() { _ = term.Restore(int(input.Fd()), terminalState) }()

	if _, err := io.WriteString(output, beginProbe+endProbe); err != nil {
		return false
	}

	replies := tty.ReadReplies(input, positionReply, func(replies []string) bool { return len(replies) >= positionReplies })

	return supports(strings.Join(replies, ""))
}

type position struct {
	row    int
	column int
}

func supports(reply string) bool {
	positions := getPositions(reply)
	if len(positions) < 3 {
		return false
	}

	first, second, third := positions[0], positions[1], positions[2]
	return first.row == second.row && second.row == third.row && second.column == first.column+2 && third.column == second.column+2
}

func getPositions(reply string) []position {
	var positions []position

	for remainingReply := reply; ; {
		start := strings.Index(remainingReply, "\x1b[")
		if start < 0 {
			break
		}
		remainingReply = remainingReply[start+2:]

		end := strings.IndexByte(remainingReply, 'R')
		if end < 0 {
			break
		}
		parameters := remainingReply[:end]
		remainingReply = remainingReply[end+1:]

		rowText, columnText, found := strings.Cut(parameters, ";")
		if !found {
			continue
		}
		row, rowError := strconv.Atoi(rowText)
		column, columnError := strconv.Atoi(columnText)
		if rowError == nil && columnError == nil && row > 0 && column > 0 {
			positions = append(positions, position{row: row, column: column})
		}
	}

	return positions
}
