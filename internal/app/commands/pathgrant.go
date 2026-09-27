package commands

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/pkg/agent"
)

const (
	pathGrantContinuationIndent = 29
	grantsAllArgument           = "all"
)

type PathGrants struct {
	Permanent      []shell.ScopedPathGrant
	DenyPatterns   []string
	IsYolo         bool
	Grant          func(path string, access pathgrant.Access) (agent.Event, error)
	Revoke         func(path string) (agent.Event, error)
	GetCurrent     func() []pathgrant.Grant
	GetPermanent   func() []shell.ScopedPathGrant
	GetCurrentCaps func() caps.Set
}

func (self PathGrants) isConfigured() bool {
	return self.Grant != nil && self.Revoke != nil && self.GetCurrent != nil
}

func (self PathGrants) getPermanent() []shell.ScopedPathGrant {
	if self.GetPermanent != nil {
		return self.GetPermanent()
	}
	return slices.Clone(self.Permanent)
}

func (self PathGrants) getCurrentCaps() caps.Set {
	if self.GetCurrentCaps != nil {
		return self.GetCurrentCaps()
	}
	return caps.All()
}

func pathGrantCommands(
	grants PathGrants,
	hostToSandbox HostToSandbox,
	sandboxToHost SandboxToHost,
) []slash.Command {
	return []slash.Command{
		exposeCommand(sandboxToHost),
		grantCommand(grants),
		grantsCommand(grants, hostToSandbox, sandboxToHost),
		revokeCommand(grants, hostToSandbox, sandboxToHost),
	}
}

func exposeCommand(sandboxToHost SandboxToHost) slash.Command {
	return slash.Command{
		Name:        "expose",
		Description: "expose a host loopback port to the sandbox",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			port, err := portgrant.ParsePort(arguments.Text)
			if err != nil {
				return slash.Usage()
			}
			if !sandboxToHost.isConfigured() {
				return errors.New("a host loopback port needs the sandbox's own network, which this session does not have")
			}
			event, err := sandboxToHost.Expose(port)
			if err != nil {
				return err
			}
			context.Emit(event)
			return nil
		},
	}.WithArgumentUsage("<port>")
}

func grantCommand(grants PathGrants) slash.Command {
	return slash.Command{
		Name:        "grant",
		Description: "grant temporary access to a path",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			accessText, path, found := strings.Cut(arguments.Text, " ")
			path = strings.TrimSpace(path)
			if !found || path == "" {
				return slash.Usage()
			}

			access, err := shell.ParseAccess(strings.TrimSpace(accessText))
			if err != nil {
				return slash.Usage()
			}
			event, err := grants.Grant(path, access)
			if err != nil {
				return err
			}
			context.Emit(event)
			return nil
		},
	}.
		WithArguments(grantFlagChoices()...).
		WithArgumentUsage(pathgrant.GrantUsage)
}

func grantFlagChoices() []string {
	choices := []pathgrant.Access{
		pathgrant.ReadAccess,
		pathgrant.ReadAccess | pathgrant.ExecAccess,
		pathgrant.ReadAccess | pathgrant.WriteAccess,
		pathgrant.ReadAccess | pathgrant.ExecAccess | pathgrant.WriteAccess,
	}

	flags := make([]string, 0, len(choices))
	for _, access := range choices {
		flags = append(flags, access.Flags())
	}

	return flags
}

func grantsCommand(grants PathGrants, hostToSandbox HostToSandbox, sandboxToHost SandboxToHost) slash.Command {
	return slash.Command{
		Name:        "grants",
		Description: "list effective access and port routes",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			isExpanded := arguments.Text == grantsAllArgument
			if arguments.Text != "" && !isExpanded {
				return slash.Usage()
			}
			context.NoticeIndented(
				formatGrants(grants, hostToSandbox, sandboxToHost, isExpanded),
				pathGrantContinuationIndent,
			)
			return nil
		},
	}.
		WithArguments(grantsAllArgument).
		WithArgumentUsage("[all]")
}

func revokeCommand(grants PathGrants, hostToSandbox HostToSandbox, sandboxToHost SandboxToHost) slash.Command {
	return slash.Command{
		Name:        "revoke",
		Description: "revoke a temporary path grant or port route",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text == "" {
				return slash.Usage()
			}
			event, err := revoke(grants, hostToSandbox, sandboxToHost, arguments.Text)
			if err != nil {
				return err
			}
			context.Emit(event)
			return nil
		},
	}.
		WithListedArguments(func() []string { return revocableSubjects(grants, hostToSandbox, sandboxToHost) }).
		WithArgumentUsage("{<path>|<port>}")
}

func revoke(
	grants PathGrants,
	hostToSandbox HostToSandbox,
	sandboxToHost SandboxToHost,
	subject string,
) (agent.Event, error) {
	if port, err := portgrant.ParsePort(subject); err == nil {
		if sandboxToHost.isConfigured() && slices.Contains(sandboxToHost.GetCurrent(), port) {
			return sandboxToHost.Revoke(port)
		}
		if hostToSandbox.isConfigured() {
			return hostToSandbox.Hide(port)
		}
		return agent.Event{}, fmt.Errorf("port %d is not exposed", port)
	}

	return grants.Revoke(subject)
}

func revocableSubjects(grants PathGrants, hostToSandbox HostToSandbox, sandboxToHost SandboxToHost) []string {
	subjects := pathGrantPaths(grants.GetCurrent())
	if hostToSandbox.isConfigured() {
		for _, port := range hostToSandbox.GetCurrent() {
			subjects = append(subjects, strconv.Itoa(int(port)))
		}
	}
	if sandboxToHost.isConfigured() {
		for _, port := range sandboxToHost.GetCurrent() {
			subjects = append(subjects, strconv.Itoa(int(port)))
		}
	}

	return subjects
}

func pathGrantPaths(grants []pathgrant.Grant) []string {
	paths := make([]string, 0, len(grants))
	for _, grant := range grants {
		paths = append(paths, grant.Path)
	}
	return paths
}

func formatGrants(
	grants PathGrants,
	hostToSandbox HostToSandbox,
	sandboxToHost SandboxToHost,
	isExpanded bool,
) string {
	effective := effectivePathGrants(grants)
	sections := make([]string, 0, 5)
	if listing := formatCapabilities(grants); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatPathGrants(effective, isExpanded); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatRestrictions(grants, effective); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatHostToSandbox(hostToSandbox); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatSandboxToHost(sandboxToHost); listing != "" {
		sections = append(sections, listing)
	}
	if len(sections) == 0 {
		return "No grants or routes."
	}

	return strings.Join(sections, "\n")
}

func formatCapabilities(grants PathGrants) string {
	if grants.GetCurrentCaps == nil {
		return ""
	}
	currentCaps := grants.getCurrentCaps()
	var entries []string
	for _, entry := range []struct {
		capability caps.Set
		text       string
	}{
		{caps.Read, "r read"},
		{caps.Shell, "x shell"},
		{caps.Write, "w workspace/home"},
		{caps.Git, "g git"},
		{caps.Network, "n network/fetch"},
		{caps.Lookup, "l lookup"},
	} {
		if currentCaps.Has(entry.capability) {
			entries = append(entries, entry.text)
		}
	}
	return "Caps: " + strings.Join(entries, " · ")
}

type effectivePathGrant struct {
	path    string
	access  pathgrant.Access
	sources []shell.GrantKind
}

func effectivePathGrants(grants PathGrants) []effectivePathGrant {
	permanent := grants.getPermanent()
	temporary := grants.GetCurrent()
	allGrants := slices.Clone(permanent)
	for _, grant := range temporary {
		access := grant.Access
		if !grants.getCurrentCaps().Has(caps.Shell) {
			access &^= pathgrant.ExecAccess
		}
		allGrants = append(allGrants, shell.ScopedPathGrant{
			Path: grant.Path, Access: access, Kind: shell.TemporaryGrant,
		})
	}

	byPath := make(map[string]effectivePathGrant, len(allGrants))
	for _, grant := range allGrants {
		effective := byPath[grant.Path]
		effective.path = grant.Path
		effective.access |= grant.Access
		if !slices.Contains(effective.sources, grant.Kind) {
			effective.sources = append(effective.sources, grant.Kind)
		}
		byPath[grant.Path] = effective
	}

	paths := slices.Sorted(maps.Keys(byPath))
	effectiveGrants := make([]effectivePathGrant, 0, len(paths))
	for _, path := range paths {
		effectiveGrants = append(effectiveGrants, byPath[path])
	}
	return effectiveGrants
}

func formatRestrictions(grants PathGrants, effective []effectivePathGrant) string {
	var restrictions []string
	if !grants.getCurrentCaps().Has(caps.Git) && hasGitProtectedWrite(effective) {
		gitRestriction := ".git needs g under writable grants"
		if grants.IsYolo {
			gitRestriction = ".git in path tools needs g; yolo shell is unrestricted"
		}
		restrictions = append(restrictions, gitRestriction)
	}
	if len(grants.DenyPatterns) > 0 {
		deniedFor := "tools + shell"
		if grants.IsYolo {
			deniedFor = "path tools only"
		}
		restrictions = append(restrictions,
			"deny "+strings.Join(grants.DenyPatterns, ", ")+" in "+deniedFor,
		)
	}
	if len(restrictions) == 0 {
		return ""
	}
	return "Limits: " + strings.Join(restrictions, " · ")
}

func hasGitProtectedWrite(grants []effectivePathGrant) bool {
	for _, grant := range grants {
		if !grant.access.Has(pathgrant.WriteAccess) {
			continue
		}
		for _, source := range grant.sources {
			if source == shell.ConfiguredGrant || source == shell.PrivateHomeGrant ||
				source == shell.TemporaryGrant || source == shell.WorkspaceGrant {
				return true
			}
		}
	}
	return false
}

func formatHostToSandbox(hostToSandbox HostToSandbox) string {
	if !hostToSandbox.isConfigured() {
		return ""
	}
	current := hostToSandbox.GetCurrent()
	if len(current) == 0 {
		return ""
	}

	entries := make([]string, 0, len(current))
	for _, port := range current {
		entries = append(entries, fmt.Sprintf("%d %s", port, hostToSandbox.GetURL(port)))
	}

	return "Host → sandbox: " + strings.Join(entries, " · ")
}

func formatSandboxToHost(sandboxToHost SandboxToHost) string {
	if !sandboxToHost.isConfigured() {
		return ""
	}
	current := sandboxToHost.GetCurrent()
	if len(current) == 0 {
		return ""
	}

	entries := make([]string, 0, len(current))
	for _, port := range current {
		entries = append(entries, fmt.Sprintf("%d 127.0.0.1:%d", port, port))
	}
	return "Sandbox → host: " + strings.Join(entries, " · ")
}

type pathGrantGroup struct {
	access  pathgrant.Access
	sources string
	paths   []string
}

func formatPathGrants(grants []effectivePathGrant, isExpanded bool) string {
	if len(grants) == 0 {
		return ""
	}

	groups := groupPathGrants(grants)
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		lines = append(lines, fmt.Sprintf(
			"  %-3s  %-20s  %s", group.access.Flags(), group.sources, formatPathGrantTargets(group, isExpanded),
		))
	}
	return "Paths:\n" + strings.Join(lines, "\n")
}

func groupPathGrants(grants []effectivePathGrant) []pathGrantGroup {
	groupsByKey := make(map[string]pathGrantGroup)
	for _, grant := range grants {
		sources := make([]string, 0, len(grant.sources))
		for _, source := range grant.sources {
			sources = append(sources, string(source))
		}
		sourceText := strings.Join(sources, " + ")
		key := sourceText + "\x00" + grant.access.Flags()
		group := groupsByKey[key]
		group.access = grant.access
		group.sources = sourceText
		group.paths = append(group.paths, grant.path)
		groupsByKey[key] = group
	}

	keys := slices.Sorted(maps.Keys(groupsByKey))
	groups := make([]pathGrantGroup, 0, len(keys))
	for _, key := range keys {
		group := groupsByKey[key]
		slices.Sort(group.paths)
		groups = append(groups, group)
	}
	return groups
}

func formatPathGrantTargets(group pathGrantGroup, isExpanded bool) string {
	if isExpanded {
		return strings.Join(compactPaths(group.paths), " ")
	}
	switch shell.GrantKind(group.sources) {
	case shell.ExecutableSearchGrant:
		return "inherited"
	case shell.GlobalSkillGrant:
		return fmt.Sprintf("%d enabled", len(group.paths))
	case shell.LanguageModuleGrant:
		return "cache"
	case shell.RuntimeGrant:
		if group.access.Has(pathgrant.WriteAccess) {
			return "ptys"
		}
		return "process + resolver"
	case shell.SystemGrant:
		switch {
		case group.access.Has(pathgrant.WriteAccess):
			return "devices"
		case group.access.Has(pathgrant.ExecAccess):
			return "executables"
		default:
			return "read-only"
		}
	case shell.ConfiguredGrant,
		shell.PrivateCacheGrant,
		shell.PrivateHomeGrant,
		shell.RepositoryMetadataGrant,
		shell.SessionDropsGrant,
		shell.TemporaryDirectoryGrant,
		shell.TemporaryGrant,
		shell.UnconfinedShellGrant,
		shell.UnixSocketGrant,
		shell.WorkspaceGrant:
	}
	return strings.Join(compactPaths(group.paths), " ")
}

func compactPaths(paths []string) []string {
	pathsByParent := make(map[string][]string)
	for _, path := range paths {
		parent := filepath.Dir(path)
		pathsByParent[parent] = append(pathsByParent[parent], filepath.Base(path))
	}

	parents := slices.Sorted(maps.Keys(pathsByParent))
	compactedPaths := make([]string, 0, len(parents))
	for _, parent := range parents {
		names := pathsByParent[parent]
		slices.Sort(names)
		if len(names) == 1 || !canUsePathBraces(names) {
			for _, name := range names {
				compactedPaths = append(compactedPaths, filepath.Join(parent, name))
			}
			continue
		}
		compactedPaths = append(compactedPaths, filepath.Join(parent, "{"+strings.Join(names, ",")+"}"))
	}
	return compactedPaths
}

func canUsePathBraces(names []string) bool {
	return !slices.ContainsFunc(names, func(name string) bool {
		return strings.ContainsAny(name, "{},")
	})
}
