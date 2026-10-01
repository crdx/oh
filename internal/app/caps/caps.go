package caps

import (
	"fmt"
	"maps"
	"slices"
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
	grantedCaps, _, err := ParseWithGroups(flags, "")
	return grantedCaps, err
}

func ParseWithGroups(flags string, groupFlags string) (Set, string, error) {
	grantedCaps := Read
	grantedGroups := make(map[string]struct{})

	for _, flag := range flags {
		label := string(flag)
		if knownCap, found := Named(label); found {
			grantedCaps |= knownCap
			continue
		}
		if strings.Contains(groupFlags, label) {
			grantedGroups[label] = struct{}{}
			continue
		}

		return 0, "", fmt.Errorf(
			"unknown capability flag %q — must be one of %q",
			label,
			AllFlags+groupFlags,
		)
	}

	return grantedCaps, strings.Join(slices.Sorted(maps.Keys(grantedGroups)), ""), nil
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

type ToolGroups map[string][]string

func (self ToolGroups) Clone() ToolGroups {
	clonedGroups := make(ToolGroups, len(self))
	for flag, toolNames := range self {
		clonedGroups[flag] = slices.Clone(toolNames)
	}
	return clonedGroups
}

func (self ToolGroups) CustomFlags() string {
	var flags []string
	for flag := range self {
		if _, isBuiltIn := Named(flag); !isBuiltIn {
			flags = append(flags, flag)
		}
	}
	slices.Sort(flags)
	return strings.Join(flags, "")
}

func (self ToolGroups) GroupOf(toolName string) string {
	for flag, toolNames := range self {
		if slices.Contains(toolNames, toolName) {
			return flag
		}
	}
	return ""
}

func (self ToolGroups) Only(toolNames []string) ToolGroups {
	includedNames := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		includedNames[name] = struct{}{}
	}

	filteredGroups := make(ToolGroups)
	for flag, groupedToolNames := range self {
		for _, name := range groupedToolNames {
			if _, isIncluded := includedNames[name]; isIncluded {
				filteredGroups[flag] = append(filteredGroups[flag], name)
			}
		}
	}
	return filteredGroups
}

type GroupStatus struct {
	Flags        string
	GrantedFlags string
}

func (self GroupStatus) Has(flag string) bool {
	return strings.Contains(self.GrantedFlags, flag)
}

type modeValue struct {
	caps          Set
	grantedGroups map[string]bool
}

func (self modeValue) clone() modeValue {
	return modeValue{caps: self.caps, grantedGroups: maps.Clone(self.grantedGroups)}
}

type Mode struct {
	state            *access.State[modeValue]
	toolGroups       ToolGroups
	activeToolGroups ToolGroups
}

func modeDefinition(toolGroups func() ToolGroups) access.Definition[modeValue] {
	return access.Definition[modeValue]{
		Clone: modeValue.clone,
		Describe: func(knownValue modeValue, current modeValue) string {
			groups := toolGroups()
			notices := changeNotices(current.caps^knownValue.caps, current.caps)
			for _, flag := range slices.Sorted(maps.Keys(groups)) {
				wasGranted := modeValueAllows(knownValue, flag)
				isGranted := modeValueAllows(current, flag)
				if wasGranted == isGranted {
					continue
				}
				notices = append(notices, toolAccessNotices(groups[flag], isGranted)...)
			}
			return strings.Join(notices, " ")
		},
	}
}

func modeValueAllows(value modeValue, flag string) bool {
	if namedCaps, isBuiltIn := Named(flag); isBuiltIn {
		return value.caps.Has(namedCaps)
	}
	return value.grantedGroups[flag]
}

func NewMode(currentCaps Set) *Mode {
	return NewModeWithGroups(currentCaps, "", nil)
}

func NewModeWithGroups(
	currentCaps Set,
	grantedGroups string,
	toolGroups ToolGroups,
	activeGroups ...ToolGroups,
) *Mode {
	value := modeValue{caps: currentCaps, grantedGroups: make(map[string]bool)}
	for _, flag := range grantedGroups {
		label := string(flag)
		if _, isKnown := toolGroups[label]; isKnown {
			value.grantedGroups[label] = true
		}
	}

	clonedGroups := toolGroups.Clone()
	clonedActiveGroups := clonedGroups
	if len(activeGroups) > 0 {
		clonedActiveGroups = activeGroups[0].Clone()
	}
	mode := &Mode{
		toolGroups:       clonedGroups,
		activeToolGroups: clonedActiveGroups,
	}
	mode.state = access.New(value, modeDefinition(func() ToolGroups { return mode.activeToolGroups }))
	return mode
}

func (self *Mode) Current() Set {
	return self.state.GetCurrent().caps
}

func (self *Mode) Toggle(whichCaps Set) {
	self.state.Change(func(current modeValue) modeValue {
		current.caps ^= whichCaps
		return current
	})
}

func (self *Mode) ToggleGroup(flag string) bool {
	if _, isBuiltIn := Named(flag); isBuiltIn {
		return false
	}
	if _, isKnown := self.activeToolGroups[flag]; !isKnown {
		return false
	}

	self.state.Change(func(current modeValue) modeValue {
		current.grantedGroups[flag] = !current.grantedGroups[flag]
		return current
	})
	return true
}

func (self *Mode) Allows(flag string) bool {
	return modeValueAllows(self.state.GetCurrent(), flag)
}

func (self *Mode) RestrictTools(toolNames []string) {
	self.activeToolGroups = self.toolGroups.Only(toolNames)
}

func (self *Mode) Groups() GroupStatus {
	return self.groupStatus(self.state.GetCurrent(), self.activeToolGroups)
}

func (self *Mode) Peek() string {
	return self.state.Peek()
}

func (self *Mode) Inject() string {
	return self.state.Inject()
}

func (self *Mode) groupStatus(current modeValue, toolGroups ToolGroups) GroupStatus {
	flags := toolGroups.CustomFlags()
	var grantedFlags strings.Builder
	for _, flag := range flags {
		if modeValueAllows(current, string(flag)) {
			grantedFlags.WriteRune(flag)
		}
	}
	return GroupStatus{Flags: flags, GrantedFlags: grantedFlags.String()}
}

func toolAccessNotices(toolNames []string, isGranted bool) []string {
	notices := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		state := "refused"
		if isGranted {
			state = "available"
		}
		notices = append(notices, fmt.Sprintf("The %s tool is now %s.", name, state))
	}
	return notices
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
		return "The workspace is now read-write."
	}

	return "The workspace is now read-only."
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
