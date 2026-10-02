package sandbox

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func rightsFor(grants []grant, path string) (uint64, bool) {
	for _, granted := range grants {
		if granted.path == path {
			return granted.rights, true
		}
	}

	return 0, false
}

func TestAPolicyIsWritableOnlyBeyondItsScratch(t *testing.T) {
	if (Policy{}).Writable() {
		t.Error("a policy granting nothing was called writable")
	}
	if (Policy{Write: []string{TmpDir}}).Writable() {
		t.Error("a policy granting only its scratch was called writable")
	}
	if !(Policy{Write: []string{TmpDir, "/elsewhere"}}).Writable() {
		t.Error("a policy granting a path of its own was not called writable")
	}
}

func TestEveryPolicyGrantsWhatACommandNeedsToStart(t *testing.T) {
	grants := Policy{}.grants()

	for _, path := range []string{"/usr", "/lib", "/etc/passwd", "/dev/null"} {
		rights, granted := rightsFor(grants, path)
		if !granted {
			t.Errorf("%s was not granted at all", path)
			continue
		}
		if rights&rightsRead != rightsRead {
			t.Errorf("%s got rights %#x, want it readable", path, rights)
		}
	}
	if _, granted := rightsFor(grants, "/proc/self"); granted {
		t.Error("/proc/self was granted, pinning it to a single process")
	}

	if rights, _ := rightsFor(grants, "/usr"); rights&accessExecute == 0 {
		t.Error("commands were granted but not made executable")
	}
	if rights, _ := rightsFor(grants, "/dev/null"); rights&accessWriteFile == 0 {
		t.Error("output cannot be thrown away")
	}
	if _, granted := rightsFor(grants, "/etc"); granted {
		t.Error("the whole of /etc was granted")
	}
}

func TestEveryPolicyCanReadSystemFontConfiguration(t *testing.T) {
	rights, granted := rightsFor(Policy{}.grants(), "/etc/fonts")
	if !granted {
		t.Fatal("system font configuration was not granted")
	}
	if rights != rightsRead {
		t.Errorf("system font configuration got rights %#x, want %#x", rights, rightsRead)
	}
}

func TestOnlyTheNamedWritablePathsResolveUnixSockets(t *testing.T) {
	grants := Policy{
		Read:    []string{"/read"},
		Write:   []string{TmpDir, "/workspace"},
		Sockets: []string{TmpDir},
	}.grants()

	if rights, _ := rightsFor(grants, TmpDir); rights&accessResolveUnix == 0 {
		t.Error("the scratch cannot resolve a socket a command of its own made")
	}
	if rights, _ := rightsFor(grants, "/workspace"); rights&accessResolveUnix != 0 {
		t.Error("the workspace can reach a socket something outside the sandbox is serving")
	}
	if rights, _ := rightsFor(grants, "/read"); rights&accessResolveUnix != 0 {
		t.Error("a readable path can resolve a socket")
	}
}

func TestASocketPathThatIsNotWritableIsRefused(t *testing.T) {
	err := (Policy{Write: []string{TmpDir}, Sockets: []string{"/elsewhere"}}).sane()
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Errorf("got %v, want a socket path outside the writable ones refused", err)
	}

	if err := (Policy{Write: []string{TmpDir}, Sockets: []string{TmpDir}}).sane(); err != nil {
		t.Errorf("got %v, want a writable socket path accepted", err)
	}
}

func TestWhatAPolicyNamesIsGrantedWithTheRightsItAsked(t *testing.T) {
	grants := Policy{
		Read:  []string{"/named-read"},
		Exec:  []string{"/named-exec"},
		Write: []string{"/named-write"},
	}.grants()

	for path, want := range map[string]uint64{
		"/named-read": rightsRead, "/named-exec": rightsExec, "/named-write": rightsWrite,
	} {
		rights, granted := rightsFor(grants, path)
		if !granted {
			t.Errorf("%s was not granted at all", path)
			continue
		}
		if rights != want {
			t.Errorf("%s got rights %#x, want %#x", path, rights, want)
		}
	}
}

func TestEveryPolicyGrantsTheProcessInformationOfItsOwnNamespace(t *testing.T) {
	rights, granted := rightsFor(Policy{}.grants(), processFilesystemPath)
	if !granted {
		t.Fatal("a private process filesystem was not granted")
	}
	if rights != rightsRead {
		t.Errorf("the private process filesystem got rights %#x, want %#x", rights, rightsRead)
	}
}

func TestEveryPolicyGrantsPseudoterminals(t *testing.T) {
	grants := Policy{}.grants()
	for _, path := range []string{"/dev/ptmx", "/dev/pts"} {
		rights, granted := rightsFor(grants, path)
		if !granted {
			t.Errorf("%s was not granted", path)
			continue
		}
		if rights != rightsWrite {
			t.Errorf("%s got rights %#x, want it writable", path, rights)
		}
	}
}

func TestEveryPolicyGrantsItsPrivateSharedMemory(t *testing.T) {
	rights, granted := rightsFor(Policy{}.grants(), SharedMemoryPath)
	if !granted {
		t.Fatal("private shared memory was not granted")
	}
	if rights != rightsWrite {
		t.Errorf("private shared memory got rights %#x, want it writable", rights)
	}
	if !slices.Contains(RuntimeWritablePaths(), SharedMemoryPath) {
		t.Error("private shared memory is granted but not listed among the runtime grants")
	}
}

func TestOnlyExplicitlyOptionalPolicyPathsMayBeMissing(t *testing.T) {
	for _, granted := range systemPathGrants {
		if !granted.isOptional {
			t.Errorf("%s is required, so a machine without it could not run a command", granted.path)
		}
	}

	policy := Policy{
		Read:          []string{"/required-read", "/optional-read"},
		OptionalPaths: []string{"/optional-read"},
	}
	for _, granted := range policy.grants() {
		switch granted.path {
		case "/required-read":
			if granted.isOptional {
				t.Error("the required path was treated as optional")
			}
		case "/optional-read":
			if !granted.isOptional {
				t.Error("the optional path was treated as required")
			}
		}
	}
}

func TestMountAccessFollowsNestedPolicyRefinements(t *testing.T) {
	policy := Policy{
		Read: []string{
			"/elsewhere",
			"/work/held",
			"/work/held/open/closed",
		},
		Write: []string{
			"/work",
			"/work/held/open",
		},
	}
	want := []mountRefinement{
		{path: "/work/held", isReadOnly: true},
		{path: "/work/held/open", isReadOnly: false},
		{path: "/work/held/open/closed", isReadOnly: true},
	}
	if got := policy.mountRefinements(); !slices.Equal(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestAnExactWriteGrantWinsOverAReadGrant(t *testing.T) {
	policy := Policy{Read: []string{"/work/held"}, Write: []string{"/work", "/work/held"}}
	if got := policy.mountRefinements(); len(got) != 0 {
		t.Errorf("got %#v, want no mount refinements", got)
	}
}

func TestAPathTheMachineLacksIsNamedOnceAndOnlyWhenItIsRequired(t *testing.T) {
	directory := t.TempDir()
	absent := filepath.Join(directory, "nowhere")

	missing := Policy{Read: []string{absent, directory}}.missingPaths()
	if !slices.Equal(missing, []string{absent}) {
		t.Errorf("got %v, want only the path that is not there", missing)
	}

	optional := Policy{Read: []string{absent}, OptionalPaths: []string{absent}}
	if missing := optional.missingPaths(); len(missing) != 0 {
		t.Errorf("got %v, want the optional missing path accepted", missing)
	}

	if missing := (Policy{Read: []string{directory}}).missingPaths(); len(missing) != 0 {
		t.Errorf("got %v, want a policy naming what exists to be accepted", missing)
	}
}

func TestAnOptionalPathMustAlsoBeGranted(t *testing.T) {
	err := (Policy{OptionalPaths: []string{"/not-granted"}}).sane()
	if err == nil || !strings.Contains(err.Error(), "is optional but is not granted") {
		t.Errorf("got %v, want an ungranted optional path refused", err)
	}
}
