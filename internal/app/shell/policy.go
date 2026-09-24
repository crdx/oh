package shell

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/job"
)

const (
	shellTimeout    = 4*time.Minute + 30*time.Second
	shellCPUPercent = 80
	shellFileSize   = 64 << 30
	shellOpenFiles  = 1 << 20
	shellProcesses  = 8192

	goBuildCacheDir  = "go-build"
	goModuleCacheDir = "go-mod"
	goLintCacheDir   = "golangci-lint"
)

func shellCPUTime() time.Duration {
	return shellTimeout * time.Duration(runtime.NumCPU()) * shellCPUPercent / 100
}

func execPaths(workspaceDir string, writableRoots ...string) []string {
	paths := []string{workspaceDir}
	unsafeRoots := append([]string{workspaceDir}, writableRoots...)

	for entry := range strings.SplitSeq(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if entry == "" {
			entry = workspaceDir
		} else if !filepath.IsAbs(entry) {
			entry = filepath.Join(workspaceDir, entry)
		}

		entry = filepath.Clean(entry)
		if isBeneathAnyPath(entry, unsafeRoots) {
			continue
		}

		info, err := os.Stat(entry)
		if err != nil || !info.IsDir() || slices.Contains(paths, entry) {
			continue
		}

		paths = append(paths, entry)
	}

	return paths
}

func isBeneathAnyPath(path string, roots []string) bool {
	cleanPath := filepath.Clean(path)
	canonicalPath := pathutil.Canonicalise(path)
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if _, isBelow := pathutil.RelativeTo(cleanRoot, cleanPath); isBelow {
			return true
		}
		if _, isBelow := pathutil.RelativeTo(pathutil.Canonicalise(root), canonicalPath); isBelow {
			return true
		}
	}
	return false
}

func furnish(homeDir string, sources []string, writableRoots []string) ([]string, error) {
	grantedPaths := make([]string, 0, len(sources))

	for _, source := range sources {
		relative, below := HomeRelativePath(source)
		if !below {
			continue
		}

		target := filepath.Join(homeDir, relative)
		homeSafetyRoots := append(slices.Clone(writableRoots), homeDir)
		if link, redirects := sandbox.FirstSymlinkBeneath(filepath.Dir(target), homeSafetyRoots); redirects {
			return nil, fmt.Errorf("the shell home at %s passes through the symbolic link %s", target, link)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, err
		}

		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}

		if err := os.Symlink(source, target); err != nil {
			return nil, err
		}

		grantedPaths = append(grantedPaths, source)
	}

	return grantedPaths, nil
}

func PrepareHomeMappings(
	workspaceDir string,
	homeDir string,
	tmpDir string,
	paths Paths,
	currentCaps caps.Set,
) error {
	cacheDir := filepath.Join(homeDir, ".cache")
	writablePaths := allWritablePaths(workspaceDir, homeDir, paths.Write, currentCaps)
	writableRoots := slices.Concat(writablePaths, []string{cacheDir, tmpDir})
	if link, redirects := sandbox.FirstSymlinkBeneath(cacheDir, append(slices.Clone(writableRoots), homeDir)); redirects {
		return fmt.Errorf("the shell cache at %s passes through the symbolic link %s", cacheDir, link)
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return fmt.Errorf("could not prepare the shared cache: %w", err)
	}
	if _, err := furnish(homeDir, paths.Home, writableRoots); err != nil {
		return fmt.Errorf("could not furnish the shell home: %w", err)
	}
	return nil
}

type supportProbe func(context.Context) error

func omitUnavailableOptionalPaths(paths Paths, optionalPaths []string) (Paths, []string) {
	var unavailablePaths []string
	optionalPaths = slices.DeleteFunc(slices.Clone(optionalPaths), func(path string) bool {
		isUnavailable := !pathutil.Exists(path)
		if isUnavailable {
			unavailablePaths = append(unavailablePaths, path)
		}
		return isUnavailable
	})
	if len(unavailablePaths) == 0 {
		return paths, optionalPaths
	}

	paths = clonePaths(paths)
	removeUnavailable := func(path string) bool { return slices.Contains(unavailablePaths, path) }
	paths.Read = slices.DeleteFunc(paths.Read, removeUnavailable)
	paths.Write = slices.DeleteFunc(paths.Write, removeUnavailable)
	paths.Exec = slices.DeleteFunc(paths.Exec, removeUnavailable)
	return paths, optionalPaths
}

func createPolicyWithOptionalPaths(
	ctx context.Context,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	extraPaths Paths,
	optionalPaths []string,
	currentCaps caps.Set,
) (sandbox.Policy, error) {
	return createPolicyWithSupportProbe(
		ctx,
		workspaceDir,
		homeDir,
		tmpDir,
		extraPaths,
		optionalPaths,
		currentCaps,
		sandbox.Supported,
	)
}

func createPolicyWithSupportProbe(
	ctx context.Context,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	extraPaths Paths,
	optionalPaths []string,
	currentCaps caps.Set,
	supportedProbe supportProbe,
) (sandbox.Policy, error) {
	extraPaths, optionalPaths = omitUnavailableOptionalPaths(extraPaths, optionalPaths)
	cacheDir := filepath.Join(homeDir, ".cache")
	writablePaths := allWritablePaths(workspaceDir, homeDir, extraPaths.Write, currentCaps)
	writableRoots := slices.Concat(writablePaths, []string{cacheDir, tmpDir})

	if link, redirects := sandbox.FirstSymlinkBeneath(cacheDir, append(slices.Clone(writableRoots), homeDir)); redirects {
		return sandbox.Policy{}, fmt.Errorf("the shell cache at %s passes through the symbolic link %s", cacheDir, link)
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return sandbox.Policy{}, fmt.Errorf("could not prepare the shell cache: %w", err)
	}

	lintCachePath := filepath.Join(".cache", goLintCacheDir)
	hostLintCachePath := filepath.Join(tmpDir, lintCachePath)
	if link, redirects := sandbox.FirstSymlinkBeneath(hostLintCachePath, writableRoots); redirects {
		return sandbox.Policy{}, fmt.Errorf(
			"the shell lint cache at %s passes through the symbolic link %s", hostLintCachePath, link,
		)
	}
	if err := os.MkdirAll(hostLintCachePath, 0o700); err != nil {
		return sandbox.Policy{}, fmt.Errorf("could not prepare the shell lint cache: %w", err)
	}

	mappedPaths, err := furnish(homeDir, extraPaths.Home, writableRoots)
	if err != nil {
		return sandbox.Policy{}, fmt.Errorf("could not furnish the shell home: %w", err)
	}

	readablePaths := slices.Concat(extraPaths.Read, extraPaths.Write, mappedPaths)

	executablePaths := slices.Concat(
		execPaths(workspaceDir, extraPaths.Write...),
		extraPaths.Exec,
		extraPaths.Path,
		[]string{homeDir, sandbox.TmpDir},
	)

	policy := sandbox.Policy{
		Deny:          slices.Clone(extraPaths.Deny),
		Read:          readablePaths,
		Write:         []string{cacheDir},
		Sockets:       []string{cacheDir, sandbox.TmpDir},
		Exec:          executablePaths,
		OptionalPaths: slices.Clone(optionalPaths),
		TmpDir:        tmpDir,

		Env: []string{
			"PATH",
			"LANG",
			"TERM",
			"USER",
		},

		SetEnv: map[string]string{
			"GIT_CONFIG_NOSYSTEM":     "1",
			"GOCACHE":                 filepath.Join(cacheDir, goBuildCacheDir),
			"GOLANGCI_LINT_CACHE":     filepath.Join(sandbox.TmpDir, lintCachePath),
			"GOMODCACHE":              filepath.Join(cacheDir, goModuleCacheDir),
			"HOME":                    homeDir,
			location.StateDirVariable: location.GetStateDir(),
			"TMPDIR":                  sandbox.TmpDir,
		},

		Timeout:      shellTimeout,
		MaxCPUTime:   shellCPUTime(),
		MaxFileSize:  shellFileSize,
		MaxOpenFiles: shellOpenFiles,
		MaxProcesses: shellProcesses,
	}

	if len(extraPaths.Path) > 0 {
		policy = policy.WithSetEnv("PATH", ShellPath(extraPaths.Path))
	}

	policy = policy.WithSetEnv("GOPROXY", "off").WithSetEnv("GOSUMDB", "off")
	modules, err := goModuleCache()
	if err != nil {
		return policy, err
	}
	if modules != "" {
		proxyDir := filepath.Join(modules, "cache", "download")
		proxyURL := (&url.URL{Scheme: "file", Path: proxyDir}).String()
		policy = policy.WithRead(proxyDir).WithSetEnv("GOPROXY", proxyURL)
	}

	if len(writablePaths) == 0 {
		return readOnlySandboxPolicy(ctx, policy, workspaceDir, homeDir, supportedProbe)
	}

	writablePolicy := grantWriteAccess(policy, writablePaths)

	if !currentCaps.Has(caps.Write) {
		writablePolicy = writablePolicy.WithRead(workspaceDir)
	}
	if !slices.Contains(writablePaths, homeDir) {
		writablePolicy = writablePolicy.WithRead(homeDir)
	}

	if !currentCaps.Has(caps.Git) {
		protectRoots := slices.Clone(extraPaths.Write)
		if currentCaps.Has(caps.Write) {
			protectRoots = append([]string{workspaceDir}, protectRoots...)
		}
		var err error
		writablePolicy, err = protectedPolicyWithOptionalRoots(
			writablePolicy,
			protectRoots,
			optionalPaths,
		)
		if err != nil {
			return policy, fmt.Errorf("could not find repository metadata to protect: %w", err)
		}
	}

	writablePolicy = writablePolicy.WithWrite(sandbox.TmpDir)
	if err := supportedProbe(ctx); err != nil {
		return sandbox.Policy{}, err
	}

	return writablePolicy, nil
}

func grantWriteAccess(policy sandbox.Policy, paths []string) sandbox.Policy {
	return policy.WithoutRead(paths...).WithWrite(paths...)
}

func readOnlySandboxPolicy(
	ctx context.Context,
	policy sandbox.Policy,
	workspaceDir string,
	homeDir string,
	supportedProbe supportProbe,
) (sandbox.Policy, error) {
	policy = policy.WithRead(workspaceDir, homeDir).WithWrite(sandbox.TmpDir)

	return policy, supportedProbe(ctx)
}

func protectedPolicy(policy sandbox.Policy, roots []string) (sandbox.Policy, error) {
	return protectedPolicyWithOptionalRoots(policy, roots, nil)
}

func protectedPolicyWithOptionalRoots(
	policy sandbox.Policy,
	roots []string,
	optionalRoots []string,
) (sandbox.Policy, error) {
	var readOnlyPaths []string
	visitedRoots := make(map[string]struct{}, len(roots))

	for _, root := range roots {
		root = filepath.Clean(root)
		if _, wasSeen := visitedRoots[root]; wasSeen {
			continue
		}
		visitedRoots[root] = struct{}{}

		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			if slices.Contains(optionalRoots, root) && !pathutil.Exists(root) {
				continue
			}
			return policy, err
		}
		if file.InGitDir(resolvedRoot) {
			if !slices.Contains(policy.Read, root) {
				readOnlyPaths = append(readOnlyPaths, root)
			}
			continue
		}

		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if path != root && entry != nil && entry.IsDir() && errors.Is(err, fs.ErrPermission) {
					if !slices.Contains(policy.Read, path) && !slices.Contains(readOnlyPaths, path) {
						readOnlyPaths = append(readOnlyPaths, path)
					}
					return filepath.SkipDir
				}
				return err
			}
			if entry.Name() != ".git" {
				return nil
			}

			if !slices.Contains(policy.Read, path) && !slices.Contains(readOnlyPaths, path) {
				readOnlyPaths = append(readOnlyPaths, path)
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			if slices.Contains(optionalRoots, root) && !pathutil.Exists(root) {
				continue
			}
			return policy, err
		}
	}

	return bash.ProtectedPolicy(policy.WithRead(readOnlyPaths...)), nil
}

var (
	ErrWithheld = errors.New(
		"shell access unavailable; ctrl+x x grants it",
	)
	ErrNetworkWithheld = errors.New(
		"host network unavailable; ctrl+x n grants it",
	)
)

func RequireSandbox(ctx context.Context) error {
	return sandboxRefusal(sandbox.Supported(ctx))
}

func sandboxRefusal(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf(
		"this machine cannot sandbox commands\n"+
			"%w\n"+
			"start a new session with --yolo to run commands directly on your machine",
		err,
	)
}

func YoloPolicy(homeDir string, tmpDir string) sandbox.Policy {
	return sandbox.Policy{
		Yolo: true,

		Env: []string{
			"PATH",
			"LANG",
			"TERM",
			"USER",
		},

		SetEnv: map[string]string{
			"GIT_CONFIG_NOSYSTEM":     "1",
			"HOME":                    homeDir,
			location.StateDirVariable: location.GetStateDir(),
			"TMPDIR":                  tmpDir,
		},

		Timeout: shellTimeout,
	}
}

func New(
	workspaceDir string,
	homeDir string,
	tmpDir string,
	pathAccess *PathAccess,
	mode *caps.Mode,
	files *file.Root,
	isYolo bool,
	approveNetwork func(context.Context, string) error,
	runner sandbox.Runner,
) tool.Tool {
	fresh := func(ctx context.Context) (sandbox.Policy, error) {
		return freshPolicy(ctx, workspaceDir, homeDir, tmpDir, pathAccess, mode, isYolo)
	}
	networkApproval := func(ctx context.Context, command string) error {
		if !mode.Current().Has(caps.Network) {
			return ErrNetworkWithheld
		}

		return approveNetwork(ctx, command)
	}

	return bash.New(files, fresh, networkApproval, runner, !isYolo)
}

func NewJob(
	manager *jobs.Manager,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	pathAccess *PathAccess,
	mode *caps.Mode,
	files *file.Root,
	isYolo bool,
	doesWake bool,
) tool.Tool {
	fresh := func(ctx context.Context) (sandbox.Policy, error) {
		policy, err := freshPolicy(ctx, workspaceDir, homeDir, tmpDir, pathAccess, mode, isYolo)
		if err != nil {
			return policy, err
		}

		policy.Timeout = 0
		policy.MaxCPUTime = 0

		return policy, nil
	}

	return job.New(manager, files, fresh, doesWake)
}

func StoppedBy(withdrawnCaps caps.Set, workspaceDir string) (func(sandbox.Policy) bool, bool) {
	if withdrawnCaps.Has(caps.Shell) {
		return func(sandbox.Policy) bool { return true }, true
	}

	if withdrawnCaps.Has(caps.Write) || withdrawnCaps.Has(caps.Git) {
		return StoppedByWritablePath(workspaceDir), true
	}

	return nil, false
}

func StoppedByWritablePath(path string) func(sandbox.Policy) bool {
	return func(policy sandbox.Policy) bool {
		return slices.Contains(policy.Write, path)
	}
}

func StoppedByPath(path string) func(sandbox.Policy) bool {
	return func(policy sandbox.Policy) bool {
		return slices.Contains(policy.Read, path) ||
			slices.Contains(policy.Write, path) ||
			slices.Contains(policy.Exec, path)
	}
}

func freshPolicy(
	ctx context.Context,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	pathAccess *PathAccess,
	mode *caps.Mode,
	isYolo bool,
) (sandbox.Policy, error) {
	currentCaps := mode.Current()

	if !currentCaps.Has(caps.Shell) {
		return sandbox.Policy{}, ErrWithheld
	}

	if isYolo {
		return YoloPolicy(homeDir, tmpDir), nil
	}

	extraPaths, optionalPaths := pathAccess.getPaths()
	policy, err := createPolicyWithOptionalPaths(
		ctx,
		workspaceDir,
		homeDir,
		tmpDir,
		extraPaths,
		optionalPaths,
		currentCaps,
	)
	if err != nil {
		if ctx.Err() != nil {
			return policy, ctx.Err()
		}

		return policy, fmt.Errorf("the shell cannot be confined: %w", err)
	}

	policy.DenyPaths, err = pathAccess.getDenyPaths(policy)
	if err != nil {
		return policy, fmt.Errorf("the shell cannot resolve sandbox denials: %w", err)
	}
	return policy, nil
}

func allWritablePaths(workspaceDir string, homeDir string, extraPaths []string, currentCaps caps.Set) []string {
	return slices.Concat(writablePaths(workspaceDir, homeDir, currentCaps), extraPaths)
}

func writablePaths(workspaceDir string, homeDir string, currentCaps caps.Set) []string {
	switch {
	case currentCaps.Has(caps.Write):
		return []string{workspaceDir, homeDir}
	case currentCaps.Has(caps.Git):
		if metadata := filepath.Join(workspaceDir, ".git"); pathutil.Exists(metadata) {
			return []string{metadata, homeDir}
		}
	}

	return nil
}
