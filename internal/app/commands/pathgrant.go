package commands

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/pkg/agent"
)

type PathGrants struct {
	Grant      func(path string, access pathgrant.Access) (agent.Event, error)
	Revoke     func(path string) (agent.Event, error)
	GetCurrent func() []pathgrant.Grant
}

func (self PathGrants) isConfigured() bool {
	return self.Grant != nil && self.Revoke != nil && self.GetCurrent != nil
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
		Description: "Expose a host loopback port to the sandbox.",
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
		Description: "Grant temporary path access, spelled with the flags r, x, and w.",
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
		WithArgumentUsage("{r|rx|rw|rxw} <path>")
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
		Description: "List temporary pathname grants and port routes in either direction.",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" {
				return slash.Usage()
			}
			context.Notice(formatGrants(grants.GetCurrent(), hostToSandbox, sandboxToHost))
			return nil
		},
	}
}

func revokeCommand(grants PathGrants, hostToSandbox HostToSandbox, sandboxToHost SandboxToHost) slash.Command {
	return slash.Command{
		Name:        "revoke",
		Description: "Revoke temporary pathname access or a port route.",
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

func formatGrants(grants []pathgrant.Grant, hostToSandbox HostToSandbox, sandboxToHost SandboxToHost) string {
	sections := make([]string, 0, 3)
	if listing := formatPathGrants(grants); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatHostToSandbox(hostToSandbox); listing != "" {
		sections = append(sections, listing)
	}
	if listing := formatSandboxToHost(sandboxToHost); listing != "" {
		sections = append(sections, listing)
	}
	if len(sections) == 0 {
		return "No temporary grants."
	}

	return strings.Join(sections, "\n")
}

func formatHostToSandbox(hostToSandbox HostToSandbox) string {
	if !hostToSandbox.isConfigured() {
		return ""
	}
	current := hostToSandbox.GetCurrent()
	if len(current) == 0 {
		return ""
	}

	lines := make([]string, 0, len(current))
	for _, port := range current {
		lines = append(lines, fmt.Sprintf("  %-5d  %s", port, hostToSandbox.GetURL(port)))
	}

	return "Host → sandbox:\n" + strings.Join(lines, "\n")
}

func formatSandboxToHost(sandboxToHost SandboxToHost) string {
	if !sandboxToHost.isConfigured() {
		return ""
	}
	current := sandboxToHost.GetCurrent()
	if len(current) == 0 {
		return ""
	}

	lines := make([]string, 0, len(current))
	for _, port := range current {
		lines = append(lines, fmt.Sprintf("  %-5d  127.0.0.1:%d", port, port))
	}
	return "Sandbox → host:\n" + strings.Join(lines, "\n")
}

func formatPathGrants(grants []pathgrant.Grant) string {
	if len(grants) == 0 {
		return ""
	}

	grantList := slices.Clone(grants)
	slices.SortFunc(grantList, func(left pathgrant.Grant, right pathgrant.Grant) int {
		return strings.Compare(left.Path, right.Path)
	})

	lines := make([]string, 0, len(grantList))
	for _, grant := range grantList {
		lines = append(lines, fmt.Sprintf("  %-3s  %s", grant.Access.Flags(), grant.Path))
	}
	return "Temporary path grants:\n" + strings.Join(lines, "\n")
}
