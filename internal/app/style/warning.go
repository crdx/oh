package style

import (
	"fmt"
	"io"
)

func WriteWarningf(to io.Writer, format string, arguments ...any) {
	if to != nil {
		_, _ = fmt.Fprintln(to, Warning("warning: "+format, arguments...))
	}
}
