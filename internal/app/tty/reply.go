package tty

import (
	"bytes"
	"errors"
	"math"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	ReplyTimeout = 250 * time.Millisecond
	maximumRead  = 4096
)

var heldInput struct {
	mutex sync.Mutex
	bytes []byte
}

func ReadReplies(input *os.File, reply *regexp.Regexp, isComplete func(replies []string) bool) []string {
	fileDescriptor := input.Fd()
	if fileDescriptor > math.MaxInt32 {
		return nil
	}

	deadline := time.Now().Add(ReplyTimeout)
	var arrival bytes.Buffer
	buffer := make([]byte, maximumRead)

	for !isComplete(findReplies(reply, arrival.Bytes())) && arrival.Len() < maximumRead {
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

		readBytes, err := input.Read(buffer[:maximumRead-arrival.Len()])
		if readBytes > 0 {
			_, _ = arrival.Write(buffer[:readBytes])
		}
		if err != nil {
			break
		}
	}

	hold(reply.ReplaceAll(arrival.Bytes(), nil))

	return findReplies(reply, arrival.Bytes())
}

func findReplies(reply *regexp.Regexp, arrival []byte) []string {
	matches := reply.FindAll(arrival, -1)
	replies := make([]string, len(matches))
	for i, match := range matches {
		replies[i] = string(match)
	}

	return replies
}

func hold(keys []byte) {
	if len(keys) == 0 {
		return
	}

	heldInput.mutex.Lock()
	defer heldInput.mutex.Unlock()

	heldInput.bytes = append(heldInput.bytes, keys...)
}

func takeHeld(buffer []byte) int {
	heldInput.mutex.Lock()
	defer heldInput.mutex.Unlock()

	count := copy(buffer, heldInput.bytes)
	heldInput.bytes = heldInput.bytes[count:]

	return count
}
