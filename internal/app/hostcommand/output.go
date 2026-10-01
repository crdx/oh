package hostcommand

import (
	"strings"
	"sync"

	"crdx.org/oh/internal/util/strutil"
)

type Output struct {
	mutex sync.Mutex
	text  strings.Builder
}

func (self *Output) Write(text []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.text.Write(text)
}

func (self *Output) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.text.String()
}

func (self *Output) LatestLine() string {
	text := self.String()

	for text != "" {
		end := strings.LastIndexAny(text, "\r\n")
		line := strings.TrimSpace(strutil.StripControl(text[end+1:]))
		if line != "" {
			return line
		}
		if end < 0 {
			break
		}
		text = text[:end]
	}

	return ""
}

func (self *Output) Settled() string {
	lines := strings.Split(self.String(), "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		lines[i] = line[strings.LastIndexByte(line, '\r')+1:]
	}

	return strings.Join(lines, "\n")
}
