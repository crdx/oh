package tool

import (
	"context"
	"fmt"
	"sync"
	"unicode/utf8"
)

const outputBufferBytes = 256 << 10

type OutputBuffer struct {
	mutex        sync.Mutex
	contents     []byte
	droppedBytes int
}

func (self *OutputBuffer) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(data) >= outputBufferBytes {
		self.droppedBytes += len(self.contents) + len(data) - outputBufferBytes
		if cap(self.contents) < outputBufferBytes {
			self.contents = make([]byte, outputBufferBytes)
		} else {
			self.contents = self.contents[:outputBufferBytes]
		}
		copy(self.contents, data[len(data)-outputBufferBytes:])
		return len(data), nil
	}

	if excess := len(self.contents) + len(data) - outputBufferBytes; excess > 0 {
		copy(self.contents, self.contents[excess:])
		self.contents = self.contents[:len(self.contents)-excess]
		self.droppedBytes += excess
	}

	requiredBytes := len(self.contents) + len(data)
	if cap(self.contents) < requiredBytes {
		capacity := min(max(2*cap(self.contents), requiredBytes), outputBufferBytes)
		contents := make([]byte, len(self.contents), capacity)
		copy(contents, self.contents)
		self.contents = contents
	}
	self.contents = append(self.contents, data...)
	return len(data), nil
}

func (self *OutputBuffer) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return string(self.contents[self.prefixBytes():])
}

func (self *OutputBuffer) DroppedBytes() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.droppedBytes + self.prefixBytes()
}

type outputKey struct{}

func WithOutput(ctx context.Context, output *OutputBuffer) context.Context {
	return context.WithValue(ctx, outputKey{}, output)
}

func LiveOutput(ctx context.Context) *OutputBuffer {
	output, _ := ctx.Value(outputKey{}).(*OutputBuffer)
	return output
}

func (self *OutputBuffer) Report() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	prefixBytes := self.prefixBytes()
	text := string(self.contents[prefixBytes:])
	if droppedBytes := self.droppedBytes + prefixBytes; droppedBytes > 0 {
		text = fmt.Sprintf("[omitted %d earlier output bytes]\n", droppedBytes) + text
	}
	return text
}

func (self *OutputBuffer) prefixBytes() int {
	if self.droppedBytes == 0 {
		return 0
	}
	count := 0
	for count < len(self.contents) && !utf8.RuneStart(self.contents[count]) {
		count++
	}
	return count
}
