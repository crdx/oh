package exposedPorts

import (
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const (
	hostToSandboxMarker = ""
	sandboxToHostMarker = "⇠ "
)

type Routes struct {
	GetPorts func() []uint16
	Hostname string
}

type state struct {
	hostToSandbox Routes
	sandboxToHost Routes
}

func New(hostToSandbox Routes, sandboxToHost Routes) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}
		return state{
			hostToSandbox: hostToSandbox,
			sandboxToHost: sandboxToHost,
		}, nil
	}
}

func (self state) Render(segment.Context) string {
	return strings.Join(self.getParts(), style.Subtle(", "))
}

func (self state) RenderWithin(_ segment.Context, cells int) string {
	parts := self.getParts()
	if len(parts) == 0 || cells <= 0 {
		return ""
	}

	all := strings.Join(parts, style.Subtle(", "))
	if style.Width(all) <= cells {
		return all
	}

	for shownCount := range slices.Backward(parts) {
		hiddenCount := len(parts) - shownCount
		candidateParts := append([]string(nil), parts[:shownCount]...)
		candidateParts = append(candidateParts, style.Subtle("+"+strconv.Itoa(hiddenCount)))
		candidate := strings.Join(candidateParts, style.Subtle(", "))
		if style.Width(candidate) <= cells {
			return candidate
		}
	}

	if cells == 1 {
		return style.Subtle("+")
	}
	return style.Subtle(width.Elide("+"+strconv.Itoa(len(parts)), cells))
}

func (self state) getParts() []string {
	var parts []string
	parts = appendParts(parts, hostToSandboxMarker, self.hostToSandbox)
	return appendParts(parts, sandboxToHostMarker, self.sandboxToHost)
}

func appendParts(parts []string, marker string, routes Routes) []string {
	for _, port := range routes.GetPorts() {
		text := style.Normal(strconv.Itoa(int(port)))
		if marker != "" {
			text = style.Subtle(marker) + text
		}
		parts = append(parts, link.RenderURL(text, portgrant.URL(routes.Hostname, port)))
	}

	return parts
}
