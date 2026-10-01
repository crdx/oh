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

const (
	groupOpening   = "["
	groupSeparator = " "
	groupClosing   = "]"
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
	grants := self.getGrants()
	paths := self.renderPaths(grants)

	return fit.Shown(len(grants), func(shownCount int) []string {
		return renderGroups(grants[:shownCount], paths)
	})
}

func (self state) renderPaths(grants []pathgrant.Grant) []string {
	baseCounts := make(map[string]int, len(grants))
	for _, grant := range grants {
		baseCounts[filepath.Base(grant.Path)]++
	}

	paths := make([]string, 0, len(grants))
	for _, grant := range grants {
		pathType := self.pathType
		if (pathType == "" || pathType == base) && baseCounts[filepath.Base(grant.Path)] > 1 {
			pathType = short
		}
		paths = append(paths, renderPath(grant.Path, pathType))
	}
	return paths
}

func renderGroups(grants []pathgrant.Grant, paths []string) []string {
	var accesses []pathgrant.Access
	pathsByAccess := map[pathgrant.Access][]string{}
	for i, grant := range grants {
		if _, isSeen := pathsByAccess[grant.Access]; !isSeen {
			accesses = append(accesses, grant.Access)
		}
		pathsByAccess[grant.Access] = append(pathsByAccess[grant.Access], paths[i])
	}

	groups := make([]string, 0, len(accesses))
	for _, access := range accesses {
		groups = append(groups, renderAccess(access)+
			style.Subtle(groupOpening)+
			strings.Join(pathsByAccess[access], groupSeparator)+
			style.Subtle(groupClosing))
	}
	return groups
}

func renderPath(path string, pathType string) string {
	shownPath := path
	switch pathType {
	case "", base:
		shownPath = filepath.Base(path)
	case short:
		shownPath = pathutil.Shorten(path)
	case full:
	}

	return link.RenderPath(style.Normal(shownPath), path)
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
