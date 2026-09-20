package graphics

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"crdx.org/oh/internal/app/tty"
)

const (
	openCommand  = "\x1b_G"
	closeCommand = "\x1b\\"

	probeCommand   = openCommand + "i=1,s=1,v=1,a=q,t=d,f=24;AAAA" + closeCommand
	deviceAttempt  = "\x1b[c"
	probeSuccess   = ";OK"
	replyTimeout   = 250 * time.Millisecond
	maximumReply   = 256
	deviceReplyEnd = 'c'
)

const (
	placeholder    = "\U0010EEEE"
	chunkSize      = 4096
	maximumImageID = 1<<24 - 1

	defaultCellWidth  = 10
	defaultCellHeight = 20
)

var lastImageID atomic.Uint32

func Detect(input *os.File, output *os.File) (int, int, bool) {
	if !tty.Is(input) || !tty.Is(output) {
		return 0, 0, false
	}

	if !isSupported(input, output) {
		return 0, 0, false
	}

	cellWidth, cellHeight := CellSize(output)

	return cellWidth, cellHeight, true
}

func isSupported(input *os.File, output *os.File) bool {
	terminalState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return false
	}
	defer func() { _ = term.Restore(int(input.Fd()), terminalState) }()

	if _, err := io.WriteString(output, probeCommand+deviceAttempt); err != nil {
		return false
	}

	return strings.Contains(readReply(input), probeSuccess)
}

func CellSize(output *os.File) (int, int) {
	size, err := unix.IoctlGetWinsize(int(output.Fd()), unix.TIOCGWINSZ)
	if err != nil || size.Xpixel == 0 || size.Ypixel == 0 || size.Col == 0 || size.Row == 0 {
		return defaultCellWidth, defaultCellHeight
	}

	return int(size.Xpixel) / int(size.Col), int(size.Ypixel) / int(size.Row)
}

func Place(picture *image.RGBA, cells int) (string, bool) {
	imageID, command, isTransmitted := Transmit(picture, cells)
	if !isTransmitted {
		return "", false
	}

	return command + Placement(imageID, cells), true
}

func Transmit(picture *image.RGBA, cells int) (int, string, bool) {
	if cells <= 0 || picture == nil || picture.Bounds().Empty() {
		return 0, "", false
	}

	payload, err := encode(picture)
	if err != nil {
		return 0, "", false
	}

	imageID := nextImageID()
	bounds := picture.Bounds()

	var placement strings.Builder

	for chunk, isLast := range chunks(payload) {
		placement.WriteString(openCommand)

		if chunk.isFirst {
			placement.WriteString(strings.Join([]string{
				"a=T", "U=1", "q=2", "f=32", "o=z", "t=d",
				"i=" + strconv.Itoa(imageID),
				"s=" + strconv.Itoa(bounds.Dx()),
				"v=" + strconv.Itoa(bounds.Dy()),
				"c=" + strconv.Itoa(cells),
				"r=1",
			}, ","))
			placement.WriteString(",")
		}

		placement.WriteString("m=")
		placement.WriteString(boolDigit(!isLast))
		placement.WriteString(";")
		placement.WriteString(chunk.text)
		placement.WriteString(closeCommand)
	}

	return imageID, placement.String(), true
}

func Placement(imageID int, cells int) string {
	if cells <= 0 {
		return ""
	}

	return placementRow(imageID, cells, 0)
}

func identifyingColour(imageID int) string {
	return fmt.Sprintf(
		"\x1b[38;2;%d;%d;%dm", imageID>>16&0xff, imageID>>8&0xff, imageID&0xff,
	)
}

func nextImageID() int {
	return int(lastImageID.Add(1)-1)%maximumImageID + 1
}

func encode(picture *image.RGBA) (string, error) {
	var buffer bytes.Buffer

	writer := zlib.NewWriter(&buffer)
	bounds := picture.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		start := picture.PixOffset(bounds.Min.X, y)
		if _, err := writer.Write(picture.Pix[start : start+bounds.Dx()*4]); err != nil {
			return "", err
		}
	}

	if err := writer.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

type chunk struct {
	text    string
	isFirst bool
}

func chunks(payload string) func(func(chunk, bool) bool) {
	return func(yield func(chunk, bool) bool) {
		for start := 0; start < len(payload); start += chunkSize {
			end := min(start+chunkSize, len(payload))

			if !yield(chunk{text: payload[start:end], isFirst: start == 0}, end == len(payload)) {
				return
			}
		}
	}
}

func boolDigit(isSet bool) string {
	if isSet {
		return "1"
	}

	return "0"
}

func readReply(input *os.File) string {
	fileDescriptor := input.Fd()
	if fileDescriptor > math.MaxInt32 {
		return ""
	}

	deadline := time.Now().Add(replyTimeout)

	var reply bytes.Buffer
	buffer := make([]byte, maximumReply)

	for !strings.ContainsRune(reply.String(), deviceReplyEnd) && reply.Len() < maximumReply {
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
