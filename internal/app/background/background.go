package background

import (
	"image/color"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tty"
)

const (
	Query            = "\x1b]11;?\x1b\\"
	deviceAttempt    = "\x1b[c"
	deviceReplyStart = "\x1b[?"
)

var probeReply = regexp.MustCompile(`\x1b\]11;[^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[\?[0-9;]*c`)

var colourReply = regexp.MustCompile(`^\x1b\]11;rgba?:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})(?:/[0-9a-fA-F]{1,4})?(?:\x07|\x1b\\)$`)

func Detect(input *os.File, output *os.File) (style.Background, bool) {
	if !tty.Is(input) || !tty.Is(output) {
		return style.DarkBackground, false
	}

	terminalState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return style.DarkBackground, false
	}
	defer func() { _ = term.Restore(int(input.Fd()), terminalState) }()

	if _, err := io.WriteString(output, Query+deviceAttempt); err != nil {
		return style.DarkBackground, false
	}

	return Read(tty.ReadReplies(input, probeReply, hasDeviceReplied))
}

func Read(replies []string) (style.Background, bool) {
	for _, reply := range replies {
		if colour, isColour := parse(reply); isColour {
			return style.BackgroundOf(colour), true
		}
	}

	return style.DarkBackground, false
}

func hasDeviceReplied(replies []string) bool {
	return slices.ContainsFunc(replies, func(reply string) bool { return strings.HasPrefix(reply, deviceReplyStart) })
}

func parse(reply string) (color.RGBA, bool) {
	match := colourReply.FindStringSubmatch(reply)
	if match == nil {
		return color.RGBA{}, false
	}

	return color.RGBA{R: channel(match[1]), G: channel(match[2]), B: channel(match[3]), A: 0xff}, true
}

func channel(digits string) uint8 {
	value, _ := strconv.ParseUint(digits, 16, 16)
	maximum := uint64(1)<<(4*len(digits)) - 1

	return uint8((value*0xff + maximum/2) / maximum) //nolint:gosec // a channel scaled to at most 0xff
}
