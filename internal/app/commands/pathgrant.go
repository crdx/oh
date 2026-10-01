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
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/agent"
)

const (
	pathGrantContinuationIndent = 29
	grantsAllArgument           = "all"
)

type PathGrants struct {
	Permanent      []shell.ScopedPathGrant
	DenyPatterns   []string
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
	forwards Forwards,
) []slash.Command {
	return []slash.Command{
		forwardCommand(forwards),
		grantCommand(grants),
		grantsCommand(grants, forwards),
		revokeCommand(grants, forwards),
	}
}

func forwardCommand(forwards Forwards) slash.Command {
	return slash.Command{
		Name:        "forward",
		Description: "forward a sandbox port to the host",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			port, err := portgrant.ParsePort(arguments.Text)
			if err != nil {
				return slash.Usage()
			}
			if !forwards.isConfigured() {
				return errors.New("a forwarded port needs the sandbox's own network, which this session does not have")
			}
			event, err := forwards.Forward(port)
			if err != nil {
				return err
			}
			context.Emit(event)
			return nil
		},
	}.WithArgumentUsage("<port>")
}

func grantCommand(grants PathGrants) slash.Command {
	flagChoices := grantFlagChoices()

	return slash.Command{
		Name:        "grant",
		Description: "grant temporary access to a path",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			accessText, pathText, _ := strings.Cut(arguments.Text, " ")
			paths, isWellFormed := slash.QuotedFields(pathText)
			if !isWellFormed || len(paths) == 0 {
				return slash.Usage()
			}

			access, err := shell.ParseAccess(strings.TrimSpace(accessText))
			if err != nil || !pathgrant.IsTemporaryAccess(access) {
				return slash.Usage()
			}
			var failures []string
			for _, path := range distinct(paths) {
				event, err := grants.Grant(path, access)
				if err != nil {
					failures = append(failures, err.Error())
					continue
				}
				context.Emit(event)
			}
			if len(failures) > 0 {
				return errors.New(strings.Join(failures, "; "))
			}
			return nil
		},
	}.
		WithArguments(flagChoices...).
		WithPathArgumentAfterMatching(isGrantAccess).
		WithArgumentUsage(pathgrant.GrantUsage).
		WithCompletionUsage("<access> <path>...")
}

func isGrantAccess(argument string) bool {
	access, err := shell.ParseAccess(argument)
	return err == nil && pathgrant.IsTemporaryAccess(access)
}

func grantFlagChoices() []string {
	choices := []pathgrant.Access{
		pathgrant.ReadAccess,
		pathgrant.ReadAccess | pathgrant.WriteAccess,
	}

	flags := make([]string, 0, len(choices))
	for _, access := range choices {
		flags = append(flags, access.Flags())
	}

	return flags
}

func grantsCommand(grants PathGrants, forwards Forwards) slash.Command {
	return slash.Command{
		Name:        "grants",
		Description: "list effective access and forwarded ports",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" && arguments.Text != grantsAllArgument {
				return slash.Usage()
			}
			listing := formatGrants(grants, forwards)
			if arguments.Text == grantsAllArgument {
				context.PlainNoticeIndented(listing, pathGrantContinuationIndent)
			} else {
				context.PlainNoticeListing(listing)
			}
			return nil
		},
	}.
		WithArguments(grantsAllArgument).
		WithArgumentUsage("[all]")
}

func revokeCommand(grants PathGrants, forwards Forwards) slash.Command {
	return slash.Command{
		Name:        "revoke",
		Description: "revoke temporary path grants or forwarded ports",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text == "" {
				return slash.Usage()
			}
			var failures []string
			for _, subject := range revokedSubjects(grants, forwards, arguments) {
				event, err := revoke(grants, forwards, subject)
				if err != nil {
					failures = append(failures, err.Error())
					continue
				}
				context.Emit(event)
			}
			if len(failures) > 0 {
				return errors.New(strings.Join(failures, "; "))
			}
			return nil
		},
	}.
		WithListedArguments(func() []string { return revocableSubjects(grants, forwards) }).
		WithManyArguments().
		WithArgumentUsage("<target>...")
}

func revokedSubjects(
	grants PathGrants,
	forwards Forwards,
	arguments slash.Arguments,
) []string {
	if slices.Contains(revocableSubjects(grants, forwards), arguments.Text) {
		return []string{arguments.Text}
	}

	return distinct(arguments.Fields)
}

func distinct(values []string) []string {
	distinctValues := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains(distinctValues, value) {
			distinctValues = append(distinctValues, value)
		}
	}
	return distinctValues
}

func revoke(
	grants PathGrants,
	forwards Forwards,
	subject string,
) (agent.Event, error) {
	if port, err := portgrant.ParsePort(subject); err == nil {
		if forwards.isConfigured() {
			return forwards.Revoke(port)
		}
		return agent.Event{}, fmt.Errorf("port %d is not forwarded", port)
	}

	return grants.Revoke(subject)
}

func revocableSubjects(grants PathGrants, forwards Forwards) []string {
	subjects := pathGrantPaths(grants.GetCurrent())
	if forwards.isConfigured() {
		for _, port := range forwards.GetCurrent() {
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
	forwards Forwards,
) string {
	effective := effectivePathGrants(grants)
	sections := make([]string, 0, 3)
	if listing := formatPathGrants(effective); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatDeniedPaths(grants); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatForwards(forwards); listing != "" {
		sections = append(sections, listing)
	}
	if len(sections) == 0 {
		return "No grants or forwarded ports."
	}

	return strings.Join(sections, "\n\n")
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
		if grants.getCurrentCaps().Has(caps.Shell) {
			access |= shell.ExecAccess
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

func formatDeniedPaths(grants PathGrants) string {
	if len(grants.DenyPatterns) == 0 {
		return ""
	}

	return style.Info("Denied:") + "\n  " + strings.Join(grants.DenyPatterns, ", ")
}

func formatForwards(forwards Forwards) string {
	var lines []string
	if forwards.isConfigured() {
		for _, port := range forwards.GetCurrent() {
			lines = append(lines, fmt.Sprintf(
				"  %d → %s",
				port,
				forwards.GetURL(port),
			))
		}
	}
	if len(lines) == 0 {
		return ""
	}

	return style.Info("Forwarded:") + "\n" + strings.Join(lines, "\n")
}

type pathGrantGroup struct {
	access  pathgrant.Access
	sources string
	paths   []string
}

func formatPathGrants(grants []effectivePathGrant) string {
	if len(grants) == 0 {
		return ""
	}

	groups := groupPathGrants(grants)
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		lines = append(lines, fmt.Sprintf(
			"  %-3s  %-20s  %s",
			group.access.Flags(),
			group.sources,
			strings.Join(compactPaths(group.paths), " "),
		))
	}
	return style.Info("Paths:") + "\n" + strings.Join(lines, "\n")
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
