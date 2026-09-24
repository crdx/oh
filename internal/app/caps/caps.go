package caps

import (
	"fmt"
	"strings"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/file"
)

type Set uint8

const (
	Read Set = 1 << iota
	Shell
	Write
	Git
	Lookup
	Network
)

var capsMap = []struct {
	flags Set
	label string
}{
	{Read, "r"},
	{Shell, "x"},
	{Write, "w"},
	{Network, "n"},
	{Git, "g"},
	{Lookup, "l"},
}

var AllFlags = All().Flags()

func All() Set {
	var allCaps Set

	for _, cap := range capsMap {
		allCaps |= cap.flags
	}

	return allCaps
}

func (self Set) Flags() string {
	var out strings.Builder

	for _, cap := range capsMap {
		if self.Has(cap.flags) {
			out.WriteString(cap.label)
		}
	}

	return out.String()
}

func (self Set) Has(want Set) bool { return self&want == want }

func (self Set) CanChangeFiles() bool { return self.Has(Write) || self.Has(Git) }

func (self Set) Flag() string {
	for _, cap := range capsMap {
		if cap.flags == self {
			return cap.label
		}
	}

	return ""
}

func Named(flag string) (Set, bool) {
	for _, knownCap := range capsMap {
		if knownCap.label == flag {
			return knownCap.flags, true
		}
	}

	return 0, false
}

func Parse(flags string) (Set, error) {
	grantedCaps := Read

	for _, flag := range flags {
		knownCap, found := Named(string(flag))
		if !found {
			return 0, fmt.Errorf(
				"unknown capability flag %q — must be one of %q",
				string(flag),
				AllFlags,
			)
		}

		grantedCaps |= knownCap
	}

	return grantedCaps, nil
}

func RefuseWrite(mode *Mode) func(name string) error {
	refuseGitWrite := RefuseGitWrite(mode)

	return func(name string) error {
		if file.InGitDir(name) {
			return refuseGitWrite(name)
		}

		if mode.Current().Has(Write) {
			return nil
		}

		return file.ErrReadOnly
	}
}

func RefuseGitWrite(mode *Mode) func(name string) error {
	return func(name string) error {
		if file.InGitDir(name) && !mode.Current().Has(Git) {
			return file.ErrGitDir
		}

		return nil
	}
}

type Mode struct {
	state *access.State[Set]
}

func modeDefinition() access.Definition[Set] {
	return access.Definition[Set]{
		Clone: func(grantedCaps Set) Set { return grantedCaps },
		Describe: func(knownCaps Set, currentCaps Set) string {
			return strings.Join(changeNotices(currentCaps^knownCaps, currentCaps), " ")
		},
	}
}

func NewMode(currentCaps Set) *Mode {
	return &Mode{state: access.New(currentCaps, modeDefinition())}
}

func (self *Mode) Current() Set {
	return self.state.GetCurrent()
}

func (self *Mode) Toggle(whichCaps Set) {
	self.state.Change(func(currentCaps Set) Set { return currentCaps ^ whichCaps })
}

func (self *Mode) Peek() string {
	return self.state.Peek()
}

func (self *Mode) Inject() string {
	return self.state.Inject()
}

func changeNotices(changedCaps Set, currentCaps Set) []string {
	var notices []string

	if changedCaps.Has(Write) {
		notices = append(notices, workspaceNotice(currentCaps.Has(Write)))
	}
	if changedCaps.Has(Shell) {
		notices = append(notices, shellNotice(currentCaps.Has(Shell)))
	}
	if changedCaps.Has(Network) {
		notices = append(notices, hostNetworkNotice(currentCaps.Has(Network)), fetchNotice(currentCaps.Has(Network)))
	}
	if changedCaps.Has(Git) {
		notices = append(notices, repositoryNotice(currentCaps.Has(Git)))
	}
	if changedCaps.Has(Lookup) {
		notices = append(notices, lookupNotice(currentCaps.Has(Lookup)))
	}

	return notices
}

func withdrawal(withdrawnCaps Set) string {
	switch {
	case withdrawnCaps.Has(Shell):
		return "the bash tool was refused"
	case withdrawnCaps.Has(Write):
		return "the workspace was made read-only"
	case withdrawnCaps.Has(Git):
		return "the .git directory was made read-only"
	default:
		return ""
	}
}

func workspaceNotice(isWritable bool) string {
	if isWritable {
		return "Workspace is now read-write."
	}

	return "Workspace is now read-only."
}

func shellNotice(isGranted bool) string {
	if isGranted {
		return "Bash is now available."
	}

	return "Bash is now refused."
}

func repositoryNotice(isWritable bool) string {
	if isWritable {
		return ".git is now read-write."
	}

	return ".git is now read-only."
}

func lookupNotice(isGranted bool) string {
	if isGranted {
		return "Lookup can now access the internet."
	}

	return "Lookup is now refused."
}

func hostNetworkNotice(isGranted bool) string {
	if isGranted {
		return "Bash may now request the host network."
	}

	return "Bash may no longer request the host network."
}

func fetchNotice(isGranted bool) string {
	if isGranted {
		return "Fetch can now access the internet."
	}

	return "Fetch is now refused."
}
