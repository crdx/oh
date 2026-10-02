package shell

import (
	"path/filepath"
	"slices"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
)

type GrantKind string

const (
	ConfiguredGrant         GrantKind = "config"
	ExecutableSearchGrant   GrantKind = "PATH"
	GlobalSkillGrant        GrantKind = "skills"
	LanguageModuleGrant     GrantKind = "modules"
	PrivateCacheGrant       GrantKind = "cache"
	PrivateHomeGrant        GrantKind = "home"
	RepositoryMetadataGrant GrantKind = "git"
	RuntimeGrant            GrantKind = "runtime"
	SessionDropsGrant       GrantKind = "drops"
	SystemGrant             GrantKind = "system"
	TemporaryDirectoryGrant GrantKind = "tmp"
	TemporaryGrant          GrantKind = "temporary"
	UnconfinedShellGrant    GrantKind = "yolo shell"
	UnixSocketGrant         GrantKind = "sockets"
	WorkspaceGrant          GrantKind = "workspace"
)

type ScopedPathGrant struct {
	Path   string
	Access Access
	Kind   GrantKind
}

func PermanentGrants(
	workspaceDirectory string,
	homeDirectory string,
	paths Paths,
	currentCaps caps.Set,
	isYolo bool,
) []ScopedPathGrant {
	isShellGranted := currentCaps.Has(caps.Shell)
	grants := configuredScopedGrants(paths, isShellGranted)

	workspaceAccess := ReadAccess
	homeAccess := ReadAccess
	if currentCaps.Has(caps.Write) {
		workspaceAccess |= WriteAccess
		homeAccess |= WriteAccess
	}
	if isShellGranted {
		workspaceAccess |= ExecAccess
		homeAccess |= ExecAccess
	}
	grants = append(grants,
		ScopedPathGrant{Path: workspaceDirectory, Access: workspaceAccess, Kind: WorkspaceGrant},
		ScopedPathGrant{Path: homeDirectory, Access: homeAccess, Kind: PrivateHomeGrant},
		ScopedPathGrant{
			Path:   filepath.Join(homeDirectory, ".cache"),
			Access: ReadAccess | WriteAccess | executableAccess(isShellGranted),
			Kind:   PrivateCacheGrant,
		},
		ScopedPathGrant{
			Path:   sandbox.TmpDir,
			Access: ReadAccess | WriteAccess | executableAccess(isShellGranted),
			Kind:   TemporaryDirectoryGrant,
		},
	)
	if isShellGranted {
		grants = append(grants,
			ScopedPathGrant{
				Path: filepath.Join(homeDirectory, ".cache"), Access: ReadAccess | WriteAccess, Kind: UnixSocketGrant,
			},
			ScopedPathGrant{Path: sandbox.TmpDir, Access: ReadAccess | WriteAccess, Kind: UnixSocketGrant},
		)
	}
	if currentCaps.Has(caps.Git) {
		grants = append(grants, ScopedPathGrant{
			Path:   filepath.Join(workspaceDirectory, "**", ".git"),
			Access: ReadAccess | WriteAccess,
			Kind:   RepositoryMetadataGrant,
		})
	}

	for _, path := range sandbox.BaselineReadablePaths() {
		if !pathutil.Exists(path) {
			continue
		}
		access := ReadAccess
		if isShellGranted && slices.Contains(sandbox.BaselineWritablePaths(), path) {
			access |= WriteAccess
		}
		if isShellGranted && slices.Contains(sandbox.BaselineExecutablePaths(), path) {
			access |= ExecAccess
		}
		grants = append(grants, ScopedPathGrant{Path: path, Access: access, Kind: SystemGrant})
	}

	for _, path := range execPaths(workspaceDirectory, paths.Write...) {
		if path == workspaceDirectory {
			continue
		}
		grants = append(grants, ScopedPathGrant{
			Path:   path,
			Access: ReadAccess | executableAccess(isShellGranted),
			Kind:   ExecutableSearchGrant,
		})
	}

	if modules, err := goModuleCache(); isShellGranted && err == nil && modules != "" {
		grants = append(grants, ScopedPathGrant{
			Path:   filepath.Join(modules, "cache", "download"),
			Access: ReadAccess,
			Kind:   LanguageModuleGrant,
		})
	}

	if isShellGranted && !isYolo {
		for _, path := range sandbox.RuntimeReadablePaths() {
			grants = append(grants, ScopedPathGrant{Path: path, Access: ReadAccess, Kind: RuntimeGrant})
		}
		for _, path := range sandbox.RuntimeWritablePaths() {
			grants = append(grants, ScopedPathGrant{
				Path:   path,
				Access: ReadAccess | WriteAccess,
				Kind:   RuntimeGrant,
			})
		}
	}
	if isShellGranted && isYolo {
		grants = append(grants, ScopedPathGrant{
			Path:   string(filepath.Separator),
			Access: ReadAccess | ExecAccess | WriteAccess,
			Kind:   UnconfinedShellGrant,
		})
	}

	return grants
}

func configuredScopedGrants(paths Paths, isShellGranted bool) []ScopedPathGrant {
	configuredGrants := paths.ConfiguredGrants()
	grants := make([]ScopedPathGrant, 0, len(configuredGrants))
	for _, grant := range configuredGrants {
		if !isShellGranted {
			grant.Access &^= ExecAccess
		}
		grants = append(grants, ScopedPathGrant{
			Path:   grant.Path,
			Access: grant.Access,
			Kind:   ConfiguredGrant,
		})
	}
	return grants
}

func SkillGrants(enabledDirectories []string, sharedDirectories []string, currentCaps caps.Set) []ScopedPathGrant {
	grants := make([]ScopedPathGrant, 0, len(enabledDirectories))
	for _, directory := range enabledDirectories {
		access := ReadAccess
		if slices.Contains(sharedDirectories, directory) {
			directory = pathutil.Canonicalise(directory)
			access |= executableAccess(currentCaps.Has(caps.Shell))
		}
		grants = append(grants, ScopedPathGrant{Path: directory, Access: access, Kind: GlobalSkillGrant})
	}
	return grants
}

func executableAccess(isShellGranted bool) Access {
	if isShellGranted {
		return ExecAccess
	}
	return 0
}
