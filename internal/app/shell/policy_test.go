package shell

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
)

const reachableDirPrefix = "io-shell-test-"

func allowNetworking(context.Context, string) error { return nil }

func TestMain(testingMain *testing.M) {
	sandbox.Init()
	os.Exit(testingMain.Run())
}

var everyCap = []caps.Set{
	0,
	caps.Write,
	caps.Git,
	caps.Write | caps.Git,
}

func createTestPolicy(
	t *testing.T,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	extraPaths Paths,
	currentCaps caps.Set,
) (sandbox.Policy, error) {
	t.Helper()

	return createTestPolicyWithOptionalPaths(
		t,
		workspaceDir,
		homeDir,
		tmpDir,
		extraPaths,
		nil,
		currentCaps,
	)
}

func createTestPolicyWithOptionalPaths(
	t *testing.T,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	extraPaths Paths,
	optionalPaths []string,
	currentCaps caps.Set,
) (sandbox.Policy, error) {
	t.Helper()

	return createPolicyWithSupportProbe(
		t.Context(),
		workspaceDir,
		homeDir,
		tmpDir,
		extraPaths,
		optionalPaths,
		currentCaps,
		func(context.Context) error { return nil },
	)
}

func reachableDir(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	if _, isCovered := pathutil.RelativeTo(sandbox.TmpDir, directory); !isCovered {
		return directory
	}

	base, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("a scratch at %s covers %s and there is nowhere else to work: %v", sandbox.TmpDir, directory, err)
	}
	if _, isCovered := pathutil.RelativeTo(sandbox.TmpDir, base); isCovered {
		t.Skipf("a scratch at %s covers both %s and %s", sandbox.TmpDir, directory, base)
	}

	created, err := os.MkdirTemp(base, reachableDirPrefix) //nolint:usetesting // the scratch covers what t.TempDir gives
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(created); err != nil {
			t.Error(err)
		}
	})

	return created
}

func newTestPathAccess(t *testing.T, files *file.Root, mode *caps.Mode) *PathAccess {
	t.Helper()

	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	return access
}

func TestOnlyACommandSaturatingTheMachineReachesTheProcessorLimit(t *testing.T) {
	limit := shellCPUTime()
	cores := runtime.NumCPU()

	saturated := shellTimeout * time.Duration(cores)
	if limit >= saturated {
		t.Errorf("got %s, want less than the %s a command could burn on every core", limit, saturated)
	}

	threeQuarters := shellTimeout * time.Duration(cores) * 3 / 4
	if limit <= threeQuarters {
		t.Errorf("got %s, want more than the %s a command using most of them would", limit, threeQuarters)
	}

	if cores > 1 && limit <= shellTimeout {
		t.Errorf("got %s, want more than the %s wall clock a command is given", limit, shellTimeout)
	}

	policy, err := createTestPolicy(t, t.TempDir(), t.TempDir(), t.TempDir(), Paths{}, 0)
	if err != nil {
		t.Fatalf("could not create the policy: %v", err)
	}

	if policy.MaxCPUTime != limit {
		t.Errorf("got %s, want the shell to carry the %s limit", policy.MaxCPUTime, limit)
	}
}

func TestAWithheldShellIsStillOfferedAndTurnsCommandsAway(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read)
	pathAccess := newTestPathAccess(t, files, mode)
	shell := New(t.TempDir(), t.TempDir(), t.TempDir(), pathAccess, mode, files, false, allowNetworking, sandbox.Direct())

	if shell.Name() != "bash" {
		t.Errorf("expected the shell to be offered as bash, got %q", shell.Name())
	}

	call, err := shell.Parse(`{"command":"echo one"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, ErrWithheld) {
		t.Errorf("expected the command to be turned away, got %v", err)
	}
}

func TestHostNetworkingRequiresItsCapability(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read | caps.Shell)
	pathAccess := newTestPathAccess(t, files, mode)
	approvalFailure := errors.New("approval reached")
	approvalCount := 0
	shell := New(
		t.TempDir(),
		t.TempDir(),
		t.TempDir(),
		pathAccess,
		mode,
		files,
		false,
		func(context.Context, string) error {
			approvalCount++
			return approvalFailure
		},
		sandbox.Direct(),
	)

	execute := func() error {
		call, parseErr := shell.Parse(`{"command":"true","network":"host"}`)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		_, execErr := call.Exec(t.Context())
		return execErr
	}

	if err := execute(); !errors.Is(err, ErrNetworkWithheld) {
		t.Errorf("without network capability got %v, want %v", err, ErrNetworkWithheld)
	}
	if approvalCount != 0 {
		t.Errorf("approval was requested %d times without the network capability", approvalCount)
	}

	mode.Toggle(caps.Network)
	if err := execute(); !errors.Is(err, approvalFailure) {
		t.Errorf("with network capability got %v, want approval to be reached", err)
	}
	if approvalCount != 1 {
		t.Errorf("approval was requested %d times, want once", approvalCount)
	}
}

func TestCommandsKeepTheirEnvironmentAfterACapabilityChange(t *testing.T) {
	workspace := reachableDir(t)
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	home := reachableDir(t)
	tmp := t.TempDir()
	if _, err := createPolicy(t.Context(), workspace, home, tmp, Paths{}, caps.Read|caps.Shell); err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read | caps.Shell)
	pathAccess := newTestPathAccess(t, files, mode)
	shell := New(workspace, home, tmp, pathAccess, mode, files, false, allowNetworking, sandbox.Direct())
	run := func() {
		call, parseErr := shell.Parse(`{"command":"printf %s \"$HOME\""}`)
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		result, execErr := call.Exec(t.Context())
		if execErr != nil {
			t.Fatal(execErr)
		}
		if result.Output != home {
			t.Errorf("got %q, want %q", result.Output, home)
		}
	}

	run()
	mode.Toggle(caps.Write)
	run()
}

func TestCommandsMayWriteRepositoryMetadataAfterGitIsGranted(t *testing.T) {
	workspace := reachableDir(t)
	metadata := filepath.Join(workspace, ".git")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}

	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	home := reachableDir(t)
	tmp := t.TempDir()
	initialCaps := caps.Read | caps.Write | caps.Shell
	if _, err := createPolicy(t.Context(), workspace, home, tmp, Paths{}, initialCaps); err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	mode := caps.NewMode(initialCaps)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	pathAccess := newTestPathAccess(t, files, mode)
	shell := New(workspace, home, tmp, pathAccess, mode, files, false, allowNetworking, sandbox.Direct())

	run := func() error {
		call, parseErr := shell.Parse(`{"command":"touch .git/proof"}`)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		_, execErr := call.Exec(t.Context())
		return execErr
	}

	if err := run(); err == nil {
		t.Fatal("expected repository metadata to remain read-only before git was granted")
	}

	mode.Toggle(caps.Git)
	if err := run(); err != nil {
		t.Fatalf("repository metadata remained read-only after git was granted: %v", err)
	}
}

func TestTemporaryPathAccessChangesTheNextShellCommand(t *testing.T) {
	workspace := reachableDir(t)
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	home := reachableDir(t)
	tmp := t.TempDir()
	externalDirectory := reachableDir(t)
	externalFile := filepath.Join(externalDirectory, "reference")
	if err := os.WriteFile(externalFile, []byte("reference text"), 0o600); err != nil {
		t.Fatal(err)
	}
	currentCaps := caps.Read | caps.Shell
	if _, err := createPolicy(t.Context(), workspace, home, tmp, Paths{Read: []string{externalDirectory}}, currentCaps); err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	mode := caps.NewMode(currentCaps)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	shellTool := New(workspace, home, tmp, access, mode, files, false, allowNetworking, sandbox.Direct())
	run := func() (string, error) {
		arguments, marshalErr := json.Marshal(map[string]string{"command": "cat " + strconv.Quote(externalFile)})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		call, parseErr := shellTool.Parse(string(arguments))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		result, execErr := call.Exec(t.Context())
		return result.Output, execErr
	}

	if _, err := run(); err == nil {
		t.Fatal("external path was readable before it was granted")
	}
	if _, err := access.Grant(externalDirectory, ReadAccess); err != nil {
		t.Fatal(err)
	}
	if output, err := run(); err != nil || output != "reference text" {
		t.Errorf("granted read got %q and %v", output, err)
	}
	access.Revoke(externalDirectory)
	if _, err := run(); err == nil {
		t.Fatal("external path remained readable after revocation")
	}
}

func TestUnavailableOptionalPathsAreOmittedFromAPolicy(t *testing.T) {
	present := t.TempDir()
	absent := filepath.Join(t.TempDir(), "gone")
	paths, optionalPaths := omitUnavailableOptionalPaths(Paths{
		Read:  []string{present, absent},
		Write: []string{absent},
		Exec:  []string{absent},
	}, []string{absent})

	if !slices.Equal(paths.Read, []string{present}) {
		t.Errorf("got read paths %v, want only %s", paths.Read, present)
	}
	if len(paths.Write) != 0 || len(paths.Exec) != 0 || len(optionalPaths) != 0 {
		t.Errorf("unavailable paths survived in %#v and %v", paths, optionalPaths)
	}
}

func TestCommandsSurviveAVanishedTemporaryGrant(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	tests := []struct {
		name          string
		grantedAccess Access
	}{
		{"read and execute", ReadAccess | ExecAccess},
		{"read and write", ReadAccess | WriteAccess},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := reachableDir(t)
			workspaceRoot, err := os.OpenRoot(workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspaceRoot.Close() }()

			mode := caps.NewMode(caps.Read | caps.Write | caps.Shell)
			files := file.New(workspaceRoot, caps.RefuseWrite(mode))
			access := newTestPathAccess(t, files, mode)
			temporaryFile := filepath.Join(reachableDir(t), "recording.flac")
			if err := os.WriteFile(temporaryFile, []byte("audio"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := access.Grant(temporaryFile, test.grantedAccess); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(temporaryFile); err != nil {
				t.Fatal(err)
			}

			shellTool := New(
				workspace,
				reachableDir(t),
				t.TempDir(),
				access,
				mode,
				files,
				false,
				allowNetworking,
				sandbox.Direct(),
			)
			call, err := shellTool.Parse(`{"command":"printf ready"}`)
			if err != nil {
				t.Fatal(err)
			}
			result, err := call.Exec(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if result.Output != "ready" {
				t.Errorf("got %q, want ready", result.Output)
			}
		})
	}
}

func TestFreshPoliciesFollowTemporaryPathAccess(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	mode := caps.NewMode(caps.Read | caps.Write)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	access, err := NewPathAccess(files, mode, Paths{})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	temporaryDirectory := t.TempDir()
	if _, err := access.Grant(temporaryDirectory, ReadAccess|WriteAccess); err != nil {
		t.Fatal(err)
	}
	extraPaths, optionalPaths := access.getPaths()
	policy, err := createTestPolicyWithOptionalPaths(
		t,
		workspace,
		t.TempDir(),
		t.TempDir(),
		extraPaths,
		optionalPaths,
		mode.Current(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(policy.Write, temporaryDirectory) {
		t.Errorf("temporary write path is absent from %#v", policy)
	}
	if !slices.Contains(policy.OptionalPaths, temporaryDirectory) {
		t.Errorf("temporary write path is required in %#v", policy)
	}

	access.Revoke(temporaryDirectory)
	extraPaths, optionalPaths = access.getPaths()
	policy, err = createTestPolicyWithOptionalPaths(
		t,
		workspace,
		t.TempDir(),
		t.TempDir(),
		extraPaths,
		optionalPaths,
		mode.Current(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(policy.Read, temporaryDirectory) ||
		slices.Contains(policy.Write, temporaryDirectory) ||
		slices.Contains(policy.OptionalPaths, temporaryDirectory) {
		t.Errorf("revoked path survived in %#v", policy)
	}
}

func TestGrantingWriteAccessRemovesExactReadOnlyMounts(t *testing.T) {
	workspace := "/workspace"
	home := "/home"
	readOnlyFile := "/workspace/reference"
	policy := grantWriteAccess(sandbox.Policy{
		Read: []string{workspace, home, readOnlyFile},
	}, []string{workspace, home})

	for _, writableRoot := range []string{workspace, home} {
		if slices.Contains(policy.Read, writableRoot) {
			t.Errorf("writable root %s is also mounted read-only: %#v", writableRoot, policy)
		}
		if !slices.Contains(policy.Write, writableRoot) {
			t.Errorf("writable root %s was not granted write access: %#v", writableRoot, policy)
		}
	}
	if !slices.Contains(policy.Read, readOnlyFile) {
		t.Errorf("nested read-only file lost its protection: %#v", policy)
	}
}

func TestTheShellMayUseItsWorkspaceAndHome(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()

	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{}, caps.Write)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	if !policy.Writable() {
		t.Fatal("the policy unexpectedly fell back to read-only")
	}

	for _, path := range []string{workspace, home} {
		if !slices.Contains(policy.Write, path) {
			t.Errorf("expected %s to be writable, got %v", path, policy.Write)
		}
	}

	for _, path := range []string{workspace, home} {
		if !slices.Contains(policy.Exec, path) {
			t.Errorf("expected artefacts in %s to be executable, got %v", path, policy.Exec)
		}
	}

	if policy.SetEnv["HOME"] != home {
		t.Errorf("got HOME %q, want %q", policy.SetEnv["HOME"], home)
	}

	if policy.SetEnv["TMPDIR"] != sandbox.TmpDir {
		t.Errorf("got TMPDIR %q, want %q", policy.SetEnv["TMPDIR"], sandbox.TmpDir)
	}
}

func TestOnlyTheScratchAndTheCacheResolveUnixSockets(t *testing.T) {
	workspace := t.TempDir()
	homeDir := t.TempDir()

	policy, err := createTestPolicy(t, workspace, homeDir, t.TempDir(), Paths{}, caps.Write)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	if !slices.Contains(policy.Sockets, sandbox.TmpDir) {
		t.Errorf("got socket paths %v, want the scratch among them", policy.Sockets)
	}
	if !slices.Contains(policy.Sockets, filepath.Join(homeDir, ".cache")) {
		t.Errorf("got socket paths %v, want the cache among them", policy.Sockets)
	}
	if slices.Contains(policy.Sockets, workspace) {
		t.Error("the workspace may resolve a socket something outside the sandbox is serving")
	}
	for _, path := range policy.Sockets {
		if !slices.Contains(policy.Write, path) {
			t.Errorf("%s may resolve sockets but is not writable", path)
		}
	}
}

func TestAnUnsupportedWritablePolicyIsRefusedRatherThanQuietlyMadeReadOnly(t *testing.T) {
	workspace := t.TempDir()
	homeDir := t.TempDir()
	unsupported := errors.New("writable policy unsupported")

	policy, err := createPolicyWithSupportProbe(
		t.Context(),
		workspace,
		homeDir,
		t.TempDir(),
		Paths{},
		nil,
		caps.Write,
		func(context.Context) error { return unsupported },
	)
	if !errors.Is(err, unsupported) {
		t.Errorf("got %v, want %v", err, unsupported)
	}
	if len(policy.Write) > 0 {
		t.Errorf("a refused policy still granted %v", policy.Write)
	}
}

func TestAnUnsupportedReadOnlyPolicyIsRejected(t *testing.T) {
	unsupported := errors.New("read-only policy unsupported")

	_, err := createPolicyWithSupportProbe(
		t.Context(),
		t.TempDir(),
		t.TempDir(),
		t.TempDir(),
		Paths{},
		nil,
		0,
		func(context.Context) error { return unsupported },
	)
	if !errors.Is(err, unsupported) {
		t.Errorf("got %v, want %v", err, unsupported)
	}
}

func TestTmpIsAlwaysWritable(t *testing.T) {
	tmp := t.TempDir()
	workspace := t.TempDir()
	home := t.TempDir()

	readOnly, err := createTestPolicy(t, workspace, home, tmp, Paths{}, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce a read-only policy here: %v", err)
	}
	if !slices.Contains(readOnly.Write, sandbox.TmpDir) {
		t.Errorf("expected writable %s, got %v", sandbox.TmpDir, readOnly.Write)
	}

	readWrite, err := createTestPolicy(t, workspace, home, tmp, Paths{}, caps.Write)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce a writable policy here: %v", err)
	}
	if !readWrite.Writable() {
		t.Fatal("the policy unexpectedly fell back to read-only")
	}
	if !slices.Contains(readWrite.Write, sandbox.TmpDir) {
		t.Errorf("expected writable %s, got %v", sandbox.TmpDir, readWrite.Write)
	}

	if readWrite.TmpDir != tmp {
		t.Errorf("got tmp dir %q, want %q", readWrite.TmpDir, tmp)
	}
	if !slices.Contains(readWrite.Exec, sandbox.TmpDir) {
		t.Errorf("expected artefacts in %s to be executable, got %v", sandbox.TmpDir, readWrite.Exec)
	}
}

func TestConfiguredPathsReachTheShellPolicy(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	readDirectory := t.TempDir()
	writeDirectory := t.TempDir()
	execDirectory := t.TempDir()
	additional := Paths{
		Read:  []string{readDirectory},
		Write: []string{writeDirectory},
		Exec:  []string{execDirectory},
	}

	readOnly, err := createTestPolicy(t, workspace, home, t.TempDir(), additional, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the configured policy here: %v", err)
	}
	for _, path := range []string{readDirectory, writeDirectory} {
		if !slices.Contains(readOnly.Read, path) {
			t.Errorf("expected %s to be readable, got %v", path, readOnly.Read)
		}
	}
	if !slices.Contains(readOnly.Exec, execDirectory) {
		t.Errorf("expected %s to be executable, got %v", execDirectory, readOnly.Exec)
	}
	if slices.Contains(readOnly.Write, writeDirectory) {
		t.Errorf("configured write path is writable without write capability: %v", readOnly.Write)
	}
	if len(readOnly.OptionalPaths) != 0 {
		t.Errorf("configured paths were made optional: %v", readOnly.OptionalPaths)
	}

	readWrite, err := createTestPolicy(t, workspace, home, t.TempDir(), additional, caps.Write)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the configured writable policy here: %v", err)
	}
	if !readWrite.Writable() {
		t.Fatal("the policy unexpectedly fell back to read-only")
	}
	if !slices.Contains(readWrite.Write, writeDirectory) {
		t.Errorf("expected %s to be writable, got %v", writeDirectory, readWrite.Write)
	}
	if slices.Contains(readWrite.Read, writeDirectory) {
		t.Errorf("configured write path remained read-only inside its write grant: %v", readWrite.Read)
	}
}

func TestCommandsOnThePathMayBeExecuted(t *testing.T) {
	workspace := t.TempDir()
	installDir := t.TempDir()
	t.Setenv("PATH", installDir)

	if paths := execPaths(workspace); !slices.Contains(paths, installDir) {
		t.Errorf("got %v, want it to include %s", paths, installDir)
	}
}

func TestPathEntriesAlreadyExecutableInTheWorkspaceNeedNoGrant(t *testing.T) {
	workspace := t.TempDir()
	installDir := filepath.Join(workspace, ".local", "share", "toolchains", "installs", "codex")
	versionDir := filepath.Join(installDir, "1.0")
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(installDir, "latest")
	if err := os.Symlink("1.0", alias); err != nil {
		t.Fatal(err)
	}

	for _, entry := range []string{"", ".", workspace, versionDir, alias, filepath.Join(".local", "share", "toolchains", "installs", "codex", "latest")} {
		t.Run(entry, func(t *testing.T) {
			t.Setenv("PATH", entry)
			if paths := execPaths(workspace); !slices.Equal(paths, []string{workspace}) {
				t.Errorf("got %v, want only the workspace grant", paths)
			}
		})
	}
}

func TestCommandsCanExecuteThroughAWorkspacePathAlias(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	workspace := reachableDir(t)
	versionDir := filepath.Join(workspace, "tools", "1.0")
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "probe"), []byte("#!/bin/sh\nprintf ready"), 0o700); err != nil { //nolint:gosec // the test needs an executable
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "tools", "latest")
	if err := os.Symlink("1.0", alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", alias+string(os.PathListSeparator)+os.Getenv("PATH"))

	policy, err := createTestPolicy(t, workspace, reachableDir(t), t.TempDir(), Paths{}, caps.Write)
	if err != nil {
		t.Fatal(err)
	}
	result, err := sandbox.Run(t.Context(), workspace, "probe", policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Output != "ready" {
		t.Errorf("got status %d and output %q, want ready", result.ExitCode, result.Output)
	}
}

func TestAPathSymlinkInsideTheWorkspaceNeverGrantsItsOutsideTarget(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	outside := workspace + "-outside"
	for _, directory := range []string{workspace, outside} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(workspace, "latest")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", alias)

	if paths := execPaths(workspace); !slices.Equal(paths, []string{workspace}) {
		t.Errorf("got %v, want no grant through the workspace symlink", paths)
	}
}

func TestGoUsesTheShellCacheAndTheHostModuleCacheAsAProxy(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	hostModules := filepath.Join(t.TempDir(), "pkg", "mod")
	proxyDir := filepath.Join(hostModules, "cache", "download")

	if err := os.MkdirAll(proxyDir, 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Setenv("GOMODCACHE", hostModules)

	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{}, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the Go cache policy here: %v", err)
	}

	cacheDir := filepath.Join(home, ".cache")
	if slices.Contains(policy.Write, home) {
		t.Errorf("the Go caches made the shell home writable: %v", policy.Write)
	}
	if !slices.Contains(policy.Write, cacheDir) {
		t.Errorf("the shell cache is not writable: %v", policy.Write)
	}
	if !slices.Contains(policy.Read, proxyDir) {
		t.Errorf("got readable paths %v, want %s", policy.Read, proxyDir)
	}

	wantEnvironment := map[string]string{
		"GOCACHE":    filepath.Join(cacheDir, goBuildCacheDir),
		"GOMODCACHE": filepath.Join(cacheDir, goModuleCacheDir),
		"GOPROXY":    (&url.URL{Scheme: "file", Path: proxyDir}).String(),
		"GOSUMDB":    "off",
	}
	for name, want := range wantEnvironment {
		if got := policy.SetEnv[name]; got != want {
			t.Errorf("got %s %q, want %q", name, got, want)
		}
	}
}

func TestTheLinterCacheBelongsToTheSessionRatherThanTheSharedHome(t *testing.T) {
	tmp := t.TempDir()

	policy, err := createTestPolicy(t, t.TempDir(), t.TempDir(), tmp, Paths{}, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the lint cache policy here: %v", err)
	}

	want := filepath.Join(sandbox.TmpDir, ".cache", goLintCacheDir)
	if got := policy.SetEnv["GOLANGCI_LINT_CACHE"]; got != want {
		t.Errorf("got GOLANGCI_LINT_CACHE %q, want %q", got, want)
	}

	if _, err := os.Stat(filepath.Join(tmp, ".cache", goLintCacheDir)); err != nil {
		t.Errorf("the lint cache was not prepared in the session tmp dir: %v", err)
	}
}

func TestAMalformedGoModuleCacheIsRejected(t *testing.T) {
	t.Setenv("GOMODCACHE", "modules")

	if _, err := goModuleCache(); err == nil {
		t.Error("expected a relative Go module cache to be rejected")
	}
}

func TestNoPolicyGrantsMoreThanItsCapsAskFor(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	metadata := filepath.Join(workspace, ".git")

	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, currentCaps := range everyCap {
		granted := writablePaths(workspace, home, currentCaps)

		if !currentCaps.Has(caps.Write) && slices.Contains(granted, workspace) {
			t.Errorf("%s: the tree is writable without w, got %v", currentCaps.Flags(), granted)
		}

		if !currentCaps.Has(caps.Write) && !currentCaps.Has(caps.Git) && len(granted) > 0 {
			t.Errorf("%s: something is writable without w or g, got %v", currentCaps.Flags(), granted)
		}

		if !currentCaps.Has(caps.Git) && slices.Contains(granted, metadata) {
			t.Errorf("%s: the metadata is writable without g, got %v", currentCaps.Flags(), granted)
		}

		if currentCaps.Has(caps.Write) && !slices.Contains(granted, workspace) {
			t.Errorf("%s: the tree is not writable with w, got %v", currentCaps.Flags(), granted)
		}
	}
}

func TestAReadOnlyShellMayChangeOnlyItsCache(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	cacheDir := filepath.Join(home, ".cache")

	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{}, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the shell cache policy here: %v", err)
	}

	if !slices.Equal(policy.Write, []string{cacheDir, sandbox.TmpDir}) {
		t.Errorf("got writable paths %v, want only the shell cache and scratch", policy.Write)
	}
	if !slices.Contains(policy.Read, home) {
		t.Errorf("got readable paths %v, want the shell home", policy.Read)
	}
}

func TestACommitOnlyShellMayChangeTheHistoryAlone(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	metadata := filepath.Join(workspace, ".git")

	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{}, caps.Git)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	if !slices.Contains(policy.Write, metadata) {
		t.Errorf("expected the metadata to be writable, got %v", policy.Write)
	}

	if slices.Contains(policy.Write, workspace) {
		t.Errorf("expected the tree itself to be read-only, got %v", policy.Write)
	}

	if !slices.Contains(policy.Read, workspace) {
		t.Errorf("expected the tree to be readable, got %v", policy.Read)
	}
}

func TestAnExactMetadataWritePathIsProtectedFromTheShell(t *testing.T) {
	repository := t.TempDir()
	target := filepath.Join(repository, ".git", "config")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "shared")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{target, alias} {
		policy, err := protectedPolicy(sandbox.Policy{Write: []string{path}}, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(policy.Read, path) {
			t.Errorf("expected %s to be protected, got %v", path, policy.Read)
		}
	}
}

func TestEveryExistingRepositoryIsProtectedFromTheShell(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	additional := t.TempDir()
	metadataPaths := []string{
		filepath.Join(workspace, ".git"),
		filepath.Join(workspace, "nested", ".git"),
		filepath.Join(additional, "nested", ".git"),
	}
	for _, metadata := range metadataPaths {
		if err := os.MkdirAll(metadata, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{
		Write: []string{additional},
	}, caps.Write)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	for _, metadata := range metadataPaths {
		if !slices.Contains(policy.Read, metadata) {
			t.Errorf("expected %s to be protected, got %v", metadata, policy.Read)
		}
	}
}

func makeUnreadable(t *testing.T, directory string, mode os.FileMode) {
	t.Helper()

	if err := os.Chmod(directory, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o700); err != nil { //nolint:gosec // the test restores directory access
			t.Error(err)
		}
	})

	if _, err := os.ReadDir(directory); err == nil {
		t.Skip("this process can read directories regardless of their permissions")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}

func TestUnreadableSubtreesAreHeldReadOnly(t *testing.T) {
	for _, mode := range []os.FileMode{0, 0o300} {
		for _, isAdditional := range []bool{false, true} {
			t.Run(strconv.FormatUint(uint64(mode), 8)+"/additional="+strconv.FormatBool(isAdditional), func(t *testing.T) {
				workspace := t.TempDir()
				additional := t.TempDir()
				root := workspace
				if isAdditional {
					root = additional
				}
				unreadable := filepath.Join(root, "forge", "data", "ssh")
				metadataPaths := []string{
					filepath.Join(root, ".git"),
					filepath.Join(root, "a", ".git"),
					filepath.Join(root, "z", ".git"),
				}
				for _, metadata := range append(slices.Clone(metadataPaths), filepath.Join(unreadable, "hidden", ".git")) {
					if err := os.MkdirAll(metadata, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				makeUnreadable(t, unreadable, mode)

				policy, err := createTestPolicy(t, workspace, t.TempDir(), t.TempDir(), Paths{
					Write: []string{additional},
				}, caps.Write)
				if err != nil {
					t.Fatalf("an unreadable nested directory blocked the shell policy: %v", err)
				}
				for _, path := range append(metadataPaths, unreadable) {
					if !slices.Contains(policy.Read, path) {
						t.Errorf("expected %s to be held read-only, got %v", path, policy.Read)
					}
				}
				if !slices.Contains(policy.Write, root) || slices.Contains(policy.Read, root) {
					t.Errorf("the rest of the tree lost write access: %#v", policy)
				}
			})
		}
	}
}

func TestCommandsCannotChangeAnUnreadableSubtree(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	workspace := reachableDir(t)
	unreadable := filepath.Join(workspace, "service")
	metadata := filepath.Join(unreadable, "repo", ".git", "config")
	if err := os.MkdirAll(filepath.Dir(metadata), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeUnreadable(t, unreadable, 0o300)

	policy, err := createTestPolicy(t, workspace, reachableDir(t), t.TempDir(), Paths{}, caps.Write)
	if err != nil {
		t.Fatal(err)
	}
	result, err := sandbox.Run(t.Context(), workspace, "printf ready > work && cat work", policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Output != "ready" {
		t.Fatalf("the rest of the workspace is unusable: %#v", result)
	}
	for _, command := range []string{
		"printf clobbered > service/repo/.git/config",
		"chmod 700 service",
		"mv service elsewhere",
	} {
		result, err := sandbox.Run(t.Context(), workspace, command, policy)
		if err != nil {
			t.Fatal(err)
		}
		if result.ExitCode == 0 {
			t.Errorf("%q was allowed", command)
		}
	}
	content, err := os.ReadFile(metadata) //nolint:gosec // the test's own metadata
	if err != nil || string(content) != "intact" {
		t.Errorf("the hidden metadata got %q and %v", content, err)
	}
}

func TestAnUnreadableProtectionRootIsStillRefused(t *testing.T) {
	root := t.TempDir()
	makeUnreadable(t, root, 0)

	if _, err := protectedPolicy(sandbox.Policy{}, []string{root}); !errors.Is(err, os.ErrPermission) {
		t.Errorf("got %v, want the unreadable root to be refused", err)
	}
}

func TestAMissingProtectionRootIsStillRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	if _, err := protectedPolicy(sandbox.Policy{}, []string{root}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("got %v, want the missing root to be refused", err)
	}
}

func TestACommitOnlyShellWithNoRepositoryMayChangeOnlyItsCache(t *testing.T) {
	workspace := t.TempDir()
	homeDir := t.TempDir()
	cacheDir := filepath.Join(homeDir, ".cache")

	policy, err := createTestPolicy(t, workspace, homeDir, t.TempDir(), Paths{}, caps.Git)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(policy.Write, []string{cacheDir, sandbox.TmpDir}) {
		t.Errorf("got writable paths %v, want only the shell cache and scratch", policy.Write)
	}
}

func userHomeFile(t *testing.T, relative string, content string) string {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestAMappedPathIsReadableAtTheSamePlaceInTheShellHome(t *testing.T) {
	source := userHomeFile(t, filepath.Join(".config", "git", "ignore"), "*.tmp\n")
	shellHome := t.TempDir()

	granted, err := furnish(shellHome, []string{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(granted, []string{source}) {
		t.Errorf("got granted paths %v, want %v", granted, []string{source})
	}

	link := filepath.Join(shellHome, ".config", "git", "ignore")
	content, err := os.ReadFile(link) //nolint:gosec // a link this test made
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "*.tmp\n" {
		t.Errorf("got %q through %s, want %q", content, link, "*.tmp\n")
	}
}

func TestNothingMappedLeavesTheShellHomeAlone(t *testing.T) {
	shellHome := t.TempDir()

	granted, err := furnish(shellHome, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(granted) > 0 {
		t.Errorf("granted %v while nothing was mapped", granted)
	}

	entries, err := os.ReadDir(shellHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("the shell home gained %d entries", len(entries))
	}
}

func TestAStaleLinkInTheShellHomeIsReplaced(t *testing.T) {
	source := userHomeFile(t, ".gitconfig", "[user]\n")
	shellHome := t.TempDir()

	link := filepath.Join(shellHome, ".gitconfig")
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), link); err != nil {
		t.Fatal(err)
	}

	if _, err := furnish(shellHome, []string{source}, nil); err != nil {
		t.Fatal(err)
	}

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != source {
		t.Errorf("the stale link still points at %q, want %q", target, source)
	}
}

func TestAMappedPathReachesTheShellPolicy(t *testing.T) {
	source := userHomeFile(t, ".gitconfig", "[user]\n")

	policy, err := createTestPolicy(
		t,
		t.TempDir(),
		t.TempDir(),
		t.TempDir(),
		Paths{Home: []string{source}},
		caps.Read,
	)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce a policy here: %v", err)
	}

	if !slices.Contains(policy.Read, source) {
		t.Errorf("expected %s to be readable, got %v", source, policy.Read)
	}
}

func TestASymlinkedCacheRefusesTheShellPolicyAtEveryWriteState(t *testing.T) {
	for name, currentCaps := range map[string]caps.Set{
		"read-only": caps.Read,
		"writable":  caps.Write,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			planted := filepath.Join(home, ".cache")
			if err := os.Symlink(t.TempDir(), planted); err != nil {
				t.Fatal(err)
			}

			_, err := createTestPolicy(t, t.TempDir(), home, t.TempDir(), Paths{}, currentCaps)
			if err == nil || !strings.Contains(err.Error(), planted) {
				t.Errorf("got %v, want the planted cache link named", err)
			}
		})
	}
}

func TestASymlinkedLintCacheRefusesTheShellPolicy(t *testing.T) {
	tmp := t.TempDir()
	victim := t.TempDir()
	planted := filepath.Join(tmp, ".cache")
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}

	_, err := createTestPolicy(t, t.TempDir(), t.TempDir(), tmp, Paths{}, caps.Read)
	if err == nil || !strings.Contains(err.Error(), planted) {
		t.Errorf("got %v, want the planted lint cache link named", err)
	}
}

func TestAFurnishedHomeThroughAModelSymlinkIsRefusedAtEveryWriteState(t *testing.T) {
	for name, currentCaps := range map[string]caps.Set{
		"read-only": caps.Read,
		"writable":  caps.Write,
	} {
		t.Run(name, func(t *testing.T) {
			source := userHomeFile(t, filepath.Join(".config", "git", "ignore"), "*.tmp\n")
			home := t.TempDir()
			planted := filepath.Join(home, ".config")
			if err := os.Symlink(t.TempDir(), planted); err != nil {
				t.Fatal(err)
			}

			_, err := createTestPolicy(
				t,
				t.TempDir(),
				home,
				t.TempDir(),
				Paths{Home: []string{source}},
				currentCaps,
			)
			if err == nil || !strings.Contains(err.Error(), planted) {
				t.Errorf("got %v, want the planted home link named", err)
			}
		})
	}
}

func TestASymlinkedCacheIsReportedToWhoeverAskedForTheCommand(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	home := t.TempDir()
	planted := filepath.Join(home, ".cache")
	if err := os.Symlink(t.TempDir(), planted); err != nil {
		t.Fatal(err)
	}

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Write | caps.Shell)
	pathAccess := newTestPathAccess(t, files, mode)
	shell := New(workspace, home, t.TempDir(), pathAccess, mode, files, false, allowNetworking, sandbox.Direct())

	call, err := shell.Parse(`{"command":"echo one"}`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = call.Exec(t.Context())
	if err == nil {
		t.Fatal("expected the command to be refused")
	}
	if !strings.Contains(err.Error(), "the shell cannot be confined") {
		t.Errorf("expected the refusal to say the shell could not be confined, got %v", err)
	}
	if !strings.Contains(err.Error(), planted) {
		t.Errorf("expected the planted link to be named, got %v", err)
	}
}

func TestAWaivedSandboxStillWithholdsAnUngrantedShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read)
	shell := New(t.TempDir(), t.TempDir(), t.TempDir(), newTestPathAccess(t, files, mode), mode, files, true, allowNetworking, sandbox.Direct())

	call, err := shell.Parse(`{"command":"echo one"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); !errors.Is(err, ErrWithheld) {
		t.Errorf("expected the command to be turned away, got %v", err)
	}
}

func TestAWaivedSandboxRunsACommandWhereTheSandboxCouldNot(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	home := t.TempDir()
	tmp := t.TempDir()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read | caps.Shell)
	shell := New(workspace, home, tmp, newTestPathAccess(t, files, mode), mode, files, true, allowNetworking, sandbox.Direct())

	call, err := shell.Parse(`{"command":"printf %s \"$HOME:$TMPDIR\""}`)
	if err != nil {
		t.Fatal(err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := home + ":" + tmp; result.Output != want {
		t.Errorf("got %q, want %q", result.Output, want)
	}
}

func TestAWaivedSandboxBoundsNothingButTheDeadline(t *testing.T) {
	policy := YoloPolicy(t.TempDir(), t.TempDir())

	if !policy.Yolo {
		t.Error("expected the policy to say the sandbox has been waived")
	}
	if policy.Timeout != shellTimeout {
		t.Errorf("got a deadline of %s, want %s", policy.Timeout, shellTimeout)
	}
	for name, bound := range map[string]int64{
		"file size": policy.MaxFileSize, "open files": policy.MaxOpenFiles, "processes": policy.MaxProcesses,
	} {
		if bound != 0 {
			t.Errorf("expected no %s limit, got %d", name, bound)
		}
	}
	if policy.MaxCPUTime != 0 {
		t.Errorf("expected no processor limit, got %s", policy.MaxCPUTime)
	}
	for name, granted := range map[string][]string{
		"read": policy.Read, "write": policy.Write, "exec": policy.Exec,
	} {
		if len(granted) != 0 {
			t.Errorf("expected nothing named as %s, got %v", name, granted)
		}
	}
}

func TestTheProcessLimitClearsAConcurrentBuild(t *testing.T) {
	const observedPeakTasks = 687

	policy, err := createTestPolicy(t, t.TempDir(), t.TempDir(), t.TempDir(), Paths{}, caps.Shell)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxProcesses < 2*observedPeakTasks {
		t.Errorf(
			"a limit of %d tasks leaves no room above the %d a concurrent build was measured to reach, "+
				"and a build that runs out of them reports no failing test at all",
			policy.MaxProcesses, observedPeakTasks,
		)
	}
}

func TestWithdrawingWriteLeavesAReadOnlyJobAlone(t *testing.T) {
	holds, doesStop := StoppedBy(caps.Write, "/workspace")
	if !doesStop {
		t.Fatal("withdrawing write stopped nothing at all")
	}

	readOnly := sandbox.Policy{Read: []string{"/workspace"}, Write: []string{"/state/home/.cache"}}
	if holds(readOnly) {
		t.Error("a read-only job was stopped by a write withdrawal")
	}

	writable := sandbox.Policy{Write: []string{"/workspace", "/state/home/.cache"}}
	if !holds(writable) {
		t.Error("a job holding the writable workspace was not stopped")
	}
}

func TestAConfiguredPathDirectoryIsExecutableAndReachesThePath(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	toolbox := t.TempDir()
	hostPath := t.TempDir()
	t.Setenv("PATH", hostPath)

	policy, err := createTestPolicy(t, workspace, home, t.TempDir(), Paths{Path: []string{toolbox}}, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the configured policy here: %v", err)
	}

	if !slices.Contains(policy.Exec, toolbox) {
		t.Errorf("expected %s to be executable, got %v", toolbox, policy.Exec)
	}

	want := hostPath + string(os.PathListSeparator) + toolbox
	if got := policy.SetEnv["PATH"]; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestThePathIsLeftAloneWithoutAConfiguredPathDirectory(t *testing.T) {
	policy, err := createTestPolicy(t, t.TempDir(), t.TempDir(), t.TempDir(), Paths{}, 0)
	if err != nil {
		t.Fatalf("the sandbox cannot enforce the configured policy here: %v", err)
	}

	if _, isOverridden := policy.SetEnv["PATH"]; isOverridden {
		t.Errorf("PATH was overridden with no sandbox.path configured: %q", policy.SetEnv["PATH"])
	}
	if !slices.Contains(policy.Env, "PATH") {
		t.Errorf("expected PATH to pass through, got %v", policy.Env)
	}
}

func TestAToolInAConfiguredPathDirectoryIsCallableByName(t *testing.T) {
	workspace := reachableDir(t)
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	toolbox := reachableDir(t)
	tool := filepath.Join(toolbox, "saymyname")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf toolbox\n"), 0o755); err != nil { //nolint:gosec // the test needs an executable
		t.Fatal(err)
	}

	home := reachableDir(t)
	tmp := t.TempDir()
	paths := Paths{Path: []string{toolbox}}
	if _, err := createPolicy(t.Context(), workspace, home, tmp, paths, caps.Read|caps.Shell); err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read | caps.Shell)

	access, err := NewPathAccess(files, mode, paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)

	shell := New(workspace, home, tmp, access, mode, files, false, allowNetworking, sandbox.Direct())
	call, err := shell.Parse(`{"command":"saymyname"}`)
	if err != nil {
		t.Fatal(err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "toolbox" {
		t.Errorf("got %q, want %q", result.Output, "toolbox")
	}
}

func TestPreparingHomeMappingsRefusesASymlinkedCache(t *testing.T) {
	home := t.TempDir()
	planted := filepath.Join(home, ".cache")
	if err := os.Symlink(t.TempDir(), planted); err != nil {
		t.Fatal(err)
	}

	err := PrepareHomeMappings(t.TempDir(), home, t.TempDir(), Paths{}, caps.Read)
	if err == nil || !strings.Contains(err.Error(), planted) {
		t.Errorf("got %v, want the planted cache link named", err)
	}
}

func TestPathEntriesInsideConfiguredWritableRootsNeedAnExplicitExecGrant(t *testing.T) {
	workspace := t.TempDir()
	writableRoot := t.TempDir()
	entry := filepath.Join(writableRoot, "bin")
	if err := os.Mkdir(entry, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "bin")
	if err := os.Symlink(entry, alias); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"direct":        entry,
		"symbolic link": alias,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PATH", path)
			if paths := execPaths(workspace, writableRoot); !slices.Equal(paths, []string{workspace}) {
				t.Errorf("got %v, want no implicit executable grant inside %s", paths, writableRoot)
			}
		})
	}
}

func TestPreparingHomeMappingsRefusesAStaleSymlinkAtEveryWriteState(t *testing.T) {
	for name, currentCaps := range map[string]caps.Set{
		"read-only": caps.Read,
		"writable":  caps.Write,
	} {
		t.Run(name, func(t *testing.T) {
			source := userHomeFile(t, filepath.Join(".config", "git", "ignore"), "*.tmp\n")
			home := t.TempDir()
			planted := filepath.Join(home, ".config")
			if err := os.Symlink(t.TempDir(), planted); err != nil {
				t.Fatal(err)
			}

			err := PrepareHomeMappings(
				t.TempDir(), home, t.TempDir(), Paths{Home: []string{source}}, currentCaps,
			)
			if err == nil || !strings.Contains(err.Error(), planted) {
				t.Errorf("got %v, want the planted home link named", err)
			}
		})
	}
}
