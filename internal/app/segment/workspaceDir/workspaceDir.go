package workspaceDir

import (
	"fmt"
	"os"
	"strings"

	"crdx.org/io/internal/app/link"
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/work"
)

const (
	full  = "full"
	base  = "base"
	short = "short"

	separator = string(os.PathSeparator)
)

type state struct {
	value   string
	address string
}

func New(workspace *work.Space) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Type string `toml:"type"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		var value string

		switch args.Type {
		case "", base:
			value = workspace.GetName()
		case short:
			value = workspace.GetShortDir()
		case full:
			value = workspace.GetDir()
		default:
			return nil, fmt.Errorf(
				"type must be omitted, %q, %q, or %q (got %q)",
				base,
				short,
				full,
				args.Type,
			)
		}

		address := ""
		if directory := workspace.GetDir(); directory != "" {
			address = link.PathURL(directory)
		}

		return state{value: value, address: address}, nil
	}
}

func (self state) Render(segment.Context) string {
	leadingPath, name := splitLeadingPath(self.value)

	text := style.Normal(name)
	if leadingPath != "" {
		text = style.Subtle(leadingPath) + text
	}

	if self.address == "" {
		return text
	}

	return link.RenderURL(text, self.address)
}

func splitLeadingPath(path string) (string, string) {
	at := strings.LastIndex(path, separator)
	if at < 0 || at == len(path)-len(separator) {
		return "", path
	}

	return path[:at+len(separator)], path[at+len(separator):]
}
