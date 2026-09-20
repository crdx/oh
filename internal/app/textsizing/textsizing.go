package textsizing

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"crdx.org/oh/internal/app/tty"
)

const (
	beginProbe = "\x1b[?1049h\x1b[?25l\r\x1b[6n\x1b]66;w=2; \a\x1b[6n\x1b]66;s=2; \a\x1b[6n"
	endProbe   = "\x1b[?25h\x1b[?1049l"

	replyTimeout = 250 * time.Millisecond
	maximumReply = 128
)

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

	return supports(readReplies(input))
}

func readReplies(input *os.File) string {
	fileDescriptor := input.Fd()
	if fileDescriptor > math.MaxInt32 {
		return ""
	}

	deadline := time.Now().Add(replyTimeout)
	var reply bytes.Buffer
	buffer := make([]byte, maximumReply)

	for strings.Count(reply.String(), "R") < 3 && reply.Len() < maximumReply {
		remainingTime := time.Until(deadline)
		if remainingTime <= 0 {
			break
		}

		pollDescriptors := []unix.PollFd{{Fd: int32(fileDescriptor), Events: unix.POLLIN}}
		readyCount, err := unix.Poll(pollDescriptors, max(1, int(remainingTime.Milliseconds())))
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			break
		}
		if readyCount == 0 || pollDescriptors[0].Revents&unix.POLLIN == 0 {
			continue
		}

		readBytes, err := input.Read(buffer[:maximumReply-reply.Len()])
		if readBytes > 0 {
			_, _ = reply.Write(buffer[:readBytes])
		}
		if err != nil {
			break
		}
	}

	return reply.String()
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
