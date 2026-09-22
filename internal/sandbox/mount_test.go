package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"crdx.org/oh/internal/sandbox/testnamespace"
	"crdx.org/oh/internal/util/pathutil"
)

func TestTheNamespaceProbeCannotRunTestsIfInitIsMissing(t *testing.T) {
	probe := namespaceProbeCommand(t.Context())

	if !slices.Contains(probe.Args, "-test.run=^$") {
		t.Errorf("the namespace probe could recursively run tests: %v", probe.Args)
	}
	if !slices.Contains(probe.Env, envProbe+"=1") {
		t.Errorf("the namespace probe does not ask Init to handle it: %v", probe.Env)
	}
}

func TestTheProbeIsBelievedWhereItsChildSaysMoreThanTheNotice(t *testing.T) {
	coverageWarning := "warning: GOCOVERDIR not set, no coverage data emitted"

	for name, output := range map[string]string{
		"a warning after the notice":  probeSucceeded + "\n" + coverageWarning + "\n",
		"a warning before the notice": coverageWarning + "\n" + probeSucceeded + "\n",
		"the notice alone":            probeSucceeded + "\n",
		"the notice behind a prefix":  notice + probeSucceeded + "\n",
	} {
		if !saysProbeSucceeded([]byte(output)) {
			t.Errorf("%s was not read as a probe that succeeded: %q", name, output)
		}
	}

	for name, output := range map[string]string{
		"nothing at all":       "",
		"a warning alone":      coverageWarning + "\n",
		"a refusal":            notice + "could not bring up the loopback interface\n",
		"the notice cut short": "sandbox probe\n",
	} {
		if saysProbeSucceeded([]byte(output)) {
			t.Errorf("%s was read as a probe that succeeded: %q", name, output)
		}
	}
}

func TestEveryCommandGetsAMountNamespace(t *testing.T) {
	if namespaceAttributes().Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected every command to be given a mount namespace of its own")
	}
}

func TestTheVirtualResolverFilesAreDistinctAbsolutePathsWithContents(t *testing.T) {
	seen := make(map[string]struct{}, len(resolverFiles))

	for _, file := range resolverFiles {
		if !filepath.IsAbs(file.path) {
			t.Errorf("got resolver file %q, want an absolute path", file.path)
		}
		if _, isDuplicate := seen[file.path]; isDuplicate {
			t.Errorf("resolver file %q is mounted twice", file.path)
		}
		seen[file.path] = struct{}{}

		if !strings.HasSuffix(file.contents, "\n") {
			t.Errorf("the contents of %q do not end in a newline", file.path)
		}
	}

	if len(resolverFiles) != 3 {
		t.Errorf("got %d resolver files, want the three of the resolver stack", len(resolverFiles))
	}
}

func TestEveryPolicyGrantsTheResolverFiles(t *testing.T) {
	var granted []string
	for _, grant := range (Policy{}).grants() {
		if !grant.isOptional {
			granted = append(granted, grant.path)
		}
	}

	for _, file := range resolverFiles {
		if !slices.Contains(granted, file.path) {
			t.Errorf("a policy granting nothing of its own lacks %s", file.path)
		}
	}
}

func TestEveryCommandGetsAPIDNamespace(t *testing.T) {
	attributes := namespaceAttributes()
	if attributes.Cloneflags&syscall.CLONE_NEWPID == 0 {
		t.Error("expected a PID namespace")
	}
	if attributes.Unshareflags != 0 {
		t.Error("expected namespaces to be created by clone")
	}
}

func TestEveryCommandOwnsItsProcessGroup(t *testing.T) {
	if !namespaceAttributes().Setpgid {
		t.Error("expected a command to own its process group")
	}
}

func TestEveryCommandDiesWithItsOwner(t *testing.T) {
	if namespaceAttributes().Pdeathsig != syscall.SIGKILL {
		t.Error("expected the command to be killed when its owner dies")
	}
}

func TestAScratchThatIsNotThereIsRefused(t *testing.T) {
	absent := Policy{TmpDir: "/scratch"}

	if missingPaths := absent.missingPaths(); len(missingPaths) != 1 || missingPaths[0] != "/scratch" {
		t.Errorf("expected the scratch to be reported missing, got %v", missingPaths)
	}
}

func TestEveryCommandGetsTheOtherNamespacesToo(t *testing.T) {
	attributes := namespaceAttributes()

	for name, flag := range map[string]uintptr{
		"user":    syscall.CLONE_NEWUSER,
		"network": syscall.CLONE_NEWNET,
		"pid":     syscall.CLONE_NEWPID,
		"ipc":     syscall.CLONE_NEWIPC,
		"uts":     syscall.CLONE_NEWUTS,
	} {
		if attributes.Cloneflags&flag == 0 {
			t.Errorf("expected a %s namespace", name)
		}
	}

	if len(attributes.UidMappings) != 1 || attributes.UidMappings[0].HostID != os.Getuid() {
		t.Errorf("got %v, want the caller mapped to root of the namespace", attributes.UidMappings)
	}
	if len(attributes.GidMappings) != 1 || attributes.GidMappings[0].HostID != os.Getgid() {
		t.Errorf("got %v, want the caller's group mapped to root of the namespace", attributes.GidMappings)
	}
}

func TestNetworkingEnabledKeepsTheHostNetworkNamespace(t *testing.T) {
	attributes := hostNetworkAttributes()

	if attributes.Cloneflags&syscall.CLONE_NEWNET != 0 {
		t.Error("networking still created an isolated network namespace")
	}
	for name, flag := range map[string]uintptr{
		"user":  syscall.CLONE_NEWUSER,
		"pid":   syscall.CLONE_NEWPID,
		"mount": syscall.CLONE_NEWNS,
		"ipc":   syscall.CLONE_NEWIPC,
		"uts":   syscall.CLONE_NEWUTS,
	} {
		if attributes.Cloneflags&flag == 0 {
			t.Errorf("networking removed the %s namespace", name)
		}
	}
}

func TestTheNamespaceProbeIsRememberedOnlyOnceItHasSucceeded(t *testing.T) {
	isUnmapped := testnamespace.IsUnmapped()
	probedNamespaces.Delete(isUnmapped)
	t.Cleanup(func() { probedNamespaces.Delete(isUnmapped) })

	err := checkNamespaces(t.Context())

	if _, wasProbed := probedNamespaces.Load(isUnmapped); wasProbed != (err == nil) {
		t.Errorf("got probe error %v with remembered=%t, want the two to agree", err, wasProbed)
	}

	if err != nil {
		return
	}

	if again := checkNamespaces(t.Context()); again != nil {
		t.Errorf("got %v from a remembered probe, want it answered without probing again", again)
	}
}

func TestARememberedProbeStillChecksThePolicyOfEveryCommand(t *testing.T) {
	if err := Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot run here: %v", err)
	}

	absent := filepath.Join(t.TempDir(), "not-there")

	for name, policy := range map[string]Policy{
		"a path that is not there": {Write: []string{absent}},
		"a relative path":          {Write: []string{"relative"}},
		"a path with a null byte":  {Write: []string{"/tmp/na\x00me"}},
	} {
		t.Run(name, func(t *testing.T) {
			for attempt := range 2 {
				if err := validate(t.Context(), policy); err == nil {
					t.Errorf("attempt %d let a refused policy through", attempt+1)
				}
			}
		})
	}
}

func TestAFailedNamespaceProbeIsNotRemembered(t *testing.T) {
	isUnmapped := testnamespace.IsUnmapped()
	probedNamespaces.Delete(isUnmapped)
	t.Cleanup(func() { probedNamespaces.Delete(isUnmapped) })

	stopped, cancel := context.WithCancel(t.Context())
	cancel()

	if err := checkNamespaces(stopped); err == nil {
		t.Fatal("expected a probe that could not run to fail")
	}

	if _, wasProbed := probedNamespaces.Load(isUnmapped); wasProbed {
		t.Error("a failed probe was remembered, so the machine would never be asked again")
	}
}

func TestARememberedProbeStillLeavesTheNamespacesToRefuseTheCommand(t *testing.T) {
	t.Setenv(testnamespace.Variable, "")

	if testnamespace.IsUnmapped() {
		t.Fatal("expected the mapped attributes once the test variable is cleared")
	}

	if probeNamespaces(t.Context()) == nil {
		t.Skip("this machine can map a namespace, so nothing here can refuse the spawn")
	}

	probedNamespaces.Store(false, struct{}{})
	t.Cleanup(func() { probedNamespaces.Delete(false) })

	if err := checkNamespaces(t.Context()); err != nil {
		t.Fatalf("expected the remembered probe to answer, got %v", err)
	}

	directory := t.TempDir()
	policy := Policy{Write: []string{directory}, Env: []string{"PATH"}}

	_, err := Direct().Run(t.Context(), directory, "echo escaped > escaped.txt", policy)
	if err == nil {
		t.Fatal("a command ran while the machine could not give it any namespaces")
	}

	if pathutil.Exists(filepath.Join(directory, "escaped.txt")) {
		t.Error("the command ran outside a namespace")
	}

	t.Logf("refused without the probe: %v", err)
}

func TestADeniedPathThatVanishedBeforeMountingIsAlreadySafe(t *testing.T) {
	mounts, err := prepareDenialMounts([]string{filepath.Join(t.TempDir(), "gone")})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 0 {
		t.Errorf("got %d mounts for a vanished path", len(mounts))
	}
}
