package call

import (
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/io/internal/app/link"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
)

func (self Label) WithHostPathAliases(roots link.Roots) Label {
	self.Subject = hostPathPrefix(self.Subject, roots)
	self.Qualifier = hostPathPrefix(self.Qualifier, roots)
	self.Emphasis.Source = hostPathPrefix(self.Emphasis.Source, roots)
	self.Continuation = slices.Clone(self.Continuation)
	for i := range self.Continuation {
		self.Continuation[i] = self.Continuation[i].WithHostPathAliases(roots)
	}

	return self
}

func hostPathPrefix(value string, roots link.Roots) string {
	if roots.Scratch == "" {
		return value
	}

	rest, hasPrefix := strings.CutPrefix(value, sandbox.TmpDir)
	switch {
	case !hasPrefix:
		return value
	case rest == "":
		return link.ScratchAlias
	case strings.HasPrefix(rest, string(filepath.Separator)):
		return link.ScratchAlias + rest
	case strings.HasPrefix(rest, " "):
		return link.ScratchAlias + rest
	default:
		return value
	}
}

func shortenPaths(rendering agent.FallbackRendering, workspace *work.Space) agent.FallbackRendering {
	primary := shortenCallRendering(tool.CallRendering{
		Subject:   rendering.Subject,
		Qualifier: rendering.Note,
		Emphasis:  rendering.Emphasis,
	}, workspace)
	rendering.Subject = primary.Subject
	rendering.Note = primary.Qualifier
	rendering.Emphasis = primary.Emphasis
	rendering.Continuation = slices.Clone(rendering.Continuation)
	for i := range rendering.Continuation {
		rendering.Continuation[i] = shortenCallRendering(rendering.Continuation[i], workspace)
	}
	return rendering
}

func shortenCallRendering(rendering tool.CallRendering, workspace *work.Space) tool.CallRendering {
	rendering.Subject = shortenPathPrefix(rendering.Subject, workspace)
	rendering.Qualifier = shortenPathPrefix(rendering.Qualifier, workspace)
	rendering.Emphasis.Source = shortenPathPrefix(rendering.Emphasis.Source, workspace)
	return rendering
}

func shortenPathPrefix(value string, workspace *work.Space) string {
	if workspaceDir := workspace.GetDir(); workspaceDir != "" {
		for _, prefix := range []string{workspaceDir, workspace.GetShortDir()} {
			rest, hasPrefix := strings.CutPrefix(value, prefix)
			switch {
			case !hasPrefix:
				continue
			case rest == "":
				return ""
			case strings.HasPrefix(rest, string(filepath.Separator)):
				return strings.TrimPrefix(rest, string(filepath.Separator))
			case strings.HasPrefix(rest, " "):
				return strings.TrimPrefix(rest, " ")
			}
		}
	}
	return pathutil.Shorten(value)
}
