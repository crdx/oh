package pathGrants

import (
	"fmt"
	"path/filepath"
	"strings"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/util/pathutil"
)

const (
	base  = "base"
	short = "short"
	full  = "full"
)

type state struct {
	getGrants func() []pathgrant.Grant
	pathType  string
}

func New(getGrants func() []pathgrant.Grant) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Type string `toml:"type"`
		}
		if err := options.Read(&args); err != nil {
			return nil, err
		}
		switch args.Type {
		case "", base, short, full:
		default:
			return nil, fmt.Errorf(
				"type must be omitted, %q, %q, or %q (got %q)",
				base,
				short,
				full,
				args.Type,
			)
		}
		return state{getGrants: getGrants, pathType: args.Type}, nil
	}
}

func (self state) Render(context segment.Context) string {
	ladder := self.Ladder(context)
	if len(ladder) == 0 {
		return ""
	}

	return ladder[0]
}

func (self state) Ladder(segment.Context) []string {
	return fit.Parts(self.getParts())
}

func (self state) getParts() []string {
	grants := self.getGrants()
	baseCounts := make(map[string]int, len(grants))
	for _, grant := range grants {
		baseCounts[filepath.Base(grant.Path)]++
	}

	parts := make([]string, 0, len(grants))
	for _, grant := range grants {
		pathType := self.pathType
		if (pathType == "" || pathType == base) && baseCounts[filepath.Base(grant.Path)] > 1 {
			pathType = short
		}
		parts = append(parts, renderGrant(grant, pathType))
	}
	return parts
}

func renderGrant(grant pathgrant.Grant, pathType string) string {
	path := grant.Path
	switch pathType {
	case "", base:
		path = filepath.Base(path)
	case short:
		path = pathutil.Shorten(path)
	case full:
	}

	return renderAccess(grant.Access) + style.Subtle(":") + link.RenderPath(style.Normal(path), grant.Path)
}

func renderAccess(access pathgrant.Access) string {
	var flags strings.Builder

	for _, right := range []struct {
		access pathgrant.Access
		render style.Style
	}{
		{pathgrant.ReadAccess, style.Read},
		{pathgrant.ExecAccess, style.ExecWhenWritable(access.Has(pathgrant.WriteAccess))},
		{pathgrant.WriteAccess, style.Write},
	} {
		if access.Has(right.access) {
			flags.WriteString(right.render(right.access.Flags()))
		}
	}

	return flags.String()
}
