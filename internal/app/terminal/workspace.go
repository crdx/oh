package terminal

import (
	"io"
	"net/url"
	"os"

	"crdx.org/oh/internal/app/tty"
)

func (self *Terminal) reportWorkspace() {
	if !tty.Is(self.writer) || self.workspaceDir == "" {
		return
	}

	hostname, _ := os.Hostname()
	address := url.URL{Scheme: "file", Host: hostname, Path: self.workspaceDir}
	_, _ = io.WriteString(self.writer, "\x1b]7;"+address.String()+"\x1b\\")
}
