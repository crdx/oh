package environment

import (
	"encoding/json"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/pkg/agent"
)

type Sandbox struct {
	DenyPatterns    []string `json:"deny,omitempty"`
	ReadPaths       []string `json:"read,omitempty"`
	WritePaths      []string `json:"write,omitempty"`
	ExecutablePaths []string `json:"exec,omitempty"`
	PathDirectories []string `json:"path,omitempty"`
	HomePaths       []string `json:"home,omitempty"`
}

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

type Snapshot struct {
	Sandbox      Sandbox `json:"sandbox"`
	Skills       []Skill `json:"skills,omitempty"`
	IsRepository bool    `json:"is_repository"`
	PortHostname string  `json:"port_hostname"`
}

func Capture(paths shell.Paths, availableSkills []skill.Skill, isRepository bool, portHostname string) Snapshot {
	skills := make([]Skill, len(availableSkills))
	for index, availableSkill := range availableSkills {
		skills[index] = Skill{
			Name:        availableSkill.Name,
			Description: availableSkill.Description,
			Location:    availableSkill.Location,
		}
	}

	return canonicalSnapshot(Snapshot{
		Sandbox: Sandbox{
			DenyPatterns:    paths.Deny,
			ReadPaths:       paths.Read,
			WritePaths:      paths.Write,
			ExecutablePaths: paths.Exec,
			PathDirectories: paths.Path,
			HomePaths:       paths.Home,
		},
		Skills:       skills,
		IsRepository: isRepository,
		PortHostname: portHostname,
	})
}

type State struct {
	state *access.State[Snapshot]
}

func NewRestored(current Snapshot, knownEnvironment Snapshot) *State {
	return &State{state: access.NewRestored(current, knownEnvironment, definition())}
}

func (self *State) Peek() string {
	return self.state.Peek()
}

func (self *State) Inject() string {
	return self.state.Inject()
}

type Restoration struct {
	State         *State
	Change        agent.Event
	IsChanged     bool
	NeedsBaseline bool
}

func Restore(createdEnvironment *Snapshot, events []agent.Event, current Snapshot) (Restoration, error) {
	current = canonicalSnapshot(current)
	knownEnvironment := current
	isKnown := false
	if createdEnvironment != nil {
		knownEnvironment = canonicalSnapshot(*createdEnvironment)
		isKnown = true
	}
	if recordedEnvironment, isFound := LastRecorded(events); isFound {
		knownEnvironment = recordedEnvironment
		isKnown = true
	}

	change, err := ChangeEvent(knownEnvironment, current)
	if err != nil {
		return Restoration{}, err
	}
	_, isChanged := Notice(change)

	return Restoration{
		State:         NewRestored(current, knownEnvironment),
		Change:        change,
		IsChanged:     isChanged,
		NeedsBaseline: !isKnown,
	}, nil
}

func definition() access.Definition[Snapshot] {
	return access.Definition[Snapshot]{
		Clone:    canonicalSnapshot,
		Describe: describeChanges,
	}
}

func canonicalSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Sandbox = Sandbox{
		DenyPatterns:    canonicalSet(snapshot.Sandbox.DenyPatterns),
		ReadPaths:       canonicalSet(snapshot.Sandbox.ReadPaths),
		WritePaths:      canonicalSet(snapshot.Sandbox.WritePaths),
		ExecutablePaths: canonicalSet(snapshot.Sandbox.ExecutablePaths),
		PathDirectories: compactOrdered(snapshot.Sandbox.PathDirectories),
		HomePaths:       canonicalSet(snapshot.Sandbox.HomePaths),
	}
	snapshot.Skills = slices.Clone(snapshot.Skills)
	slices.SortFunc(snapshot.Skills, func(left Skill, right Skill) int {
		if byLocation := strings.Compare(left.Location, right.Location); byLocation != 0 {
			return byLocation
		}
		return strings.Compare(left.Name, right.Name)
	})
	snapshot.Skills = slices.Compact(snapshot.Skills)
	return snapshot
}

func canonicalSet(values []string) []string {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}

func compactOrdered(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	compactedValues := make([]string, 0, len(values))
	for _, value := range values {
		if _, isSeen := seen[value]; isSeen {
			continue
		}
		seen[value] = struct{}{}
		compactedValues = append(compactedValues, value)
	}
	return compactedValues
}

func describeChanges(knownEnvironment Snapshot, current Snapshot) string {
	return strings.Join(changeNotices(knownEnvironment, current, modelSkillWording), " ")
}

func changeNotices(
	knownEnvironment Snapshot,
	current Snapshot,
	wording skillWording,
) []string {
	var notices []string
	notices = append(notices, setChangeNotices("Sandbox deny patterns", knownEnvironment.Sandbox.DenyPatterns, current.Sandbox.DenyPatterns)...)
	if len(difference(current.Sandbox.DenyPatterns, knownEnvironment.Sandbox.DenyPatterns)) > 0 {
		notices = append(notices, "Paths matching a sandbox deny pattern appear empty and unreadable; treat them as sandbox artefacts and ask for access only when needed.")
	}
	notices = append(notices, setChangeNotices("Configured read-only paths", knownEnvironment.Sandbox.ReadPaths, current.Sandbox.ReadPaths)...)
	notices = append(notices, setChangeNotices("Configured read-write paths", knownEnvironment.Sandbox.WritePaths, current.Sandbox.WritePaths)...)
	notices = append(notices, setChangeNotices("Configured executable paths", knownEnvironment.Sandbox.ExecutablePaths, current.Sandbox.ExecutablePaths)...)
	if !slices.Equal(knownEnvironment.Sandbox.PathDirectories, current.Sandbox.PathDirectories) {
		notices = append(notices, "Configured PATH directories are now, in order: "+listedOrNone(current.Sandbox.PathDirectories)+".")
	}
	notices = append(notices, setChangeNotices("Configured home paths", knownEnvironment.Sandbox.HomePaths, current.Sandbox.HomePaths)...)
	notices = append(notices, skillChangeNotices(knownEnvironment.Skills, current.Skills, wording)...)
	if knownEnvironment.IsRepository != current.IsRepository {
		if current.IsRepository {
			notices = append(notices, "The workspace is now a Git repository.")
		} else {
			notices = append(notices, "The workspace is no longer a Git repository.")
		}
	}
	if knownEnvironment.PortHostname != current.PortHostname {
		notices = append(notices,
			"Host-facing URLs for exposed sandbox ports now use "+markdown.CodeSpan(current.PortHostname)+
				" instead of "+markdown.CodeSpan(knownEnvironment.PortHostname)+".",
		)
	}
	return notices
}

func setChangeNotices(subject string, knownValues []string, current []string) []string {
	removedValues := difference(knownValues, current)
	addedValues := difference(current, knownValues)
	slices.Sort(removedValues)
	slices.Sort(addedValues)

	var notices []string
	if len(addedValues) > 0 {
		notices = append(notices, subject+" added: "+listed(addedValues)+".")
	}
	if len(removedValues) > 0 {
		notices = append(notices, subject+" removed: "+listed(removedValues)+".")
	}
	return notices
}

func difference(left []string, right []string) []string {
	rightValues := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightValues[value] = struct{}{}
	}

	var differentValues []string
	for _, value := range left {
		if _, isPresent := rightValues[value]; !isPresent {
			differentValues = append(differentValues, value)
		}
	}
	return differentValues
}

func listed(values []string) string {
	markedValues := make([]string, len(values))
	for index, value := range values {
		markedValues[index] = markdown.CodeSpan(value)
	}
	return strings.Join(markedValues, ", ")
}

func listedOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return listed(values)
}

func skillChangeNotices(
	knownSkills []Skill,
	current []Skill,
	wording skillWording,
) []string {
	knownByLocation := make(map[string]Skill, len(knownSkills))
	for _, foundSkill := range knownSkills {
		knownByLocation[foundSkill.Location] = foundSkill
	}
	currentByLocation := make(map[string]Skill, len(current))
	for _, foundSkill := range current {
		currentByLocation[foundSkill.Location] = foundSkill
	}

	var addedSkills, removedSkills, changedSkills []string
	for _, foundSkill := range current {
		previous, wasKnown := knownByLocation[foundSkill.Location]
		switch {
		case !wasKnown:
			addedSkills = append(addedSkills, wording.addedSkill(foundSkill))
		case previous != foundSkill:
			changedSkills = append(changedSkills, wording.changedSkill(previous, foundSkill))
		}
	}
	for _, foundSkill := range knownSkills {
		if _, isCurrent := currentByLocation[foundSkill.Location]; !isCurrent {
			removedSkills = append(removedSkills, identifySkill(foundSkill))
		}
	}

	var notices []string
	if len(addedSkills) > 0 {
		notices = append(notices, "Skills now available: "+strings.Join(addedSkills, "; ")+".")
	}
	if len(removedSkills) > 0 {
		notices = append(notices, "Skills no longer available: "+strings.Join(removedSkills, "; ")+".")
	}
	if len(changedSkills) > 0 {
		notices = append(notices, "Skills changed: "+strings.Join(changedSkills, "; ")+".")
	}
	return notices
}

type skillWording struct {
	addedSkill   func(Skill) string
	changedSkill func(Skill, Skill) string
}

var (
	userSkillWording  = skillWording{addedSkill: identifySkill, changedSkill: identifySkillChange}
	modelSkillWording = skillWording{addedSkill: describeSkill, changedSkill: describeSkillChange}
)

func identifySkill(foundSkill Skill) string {
	return markdown.CodeSpan(foundSkill.Name) + " at " + markdown.CodeSpan(foundSkill.Location)
}

func describeSkill(foundSkill Skill) string {
	return identifySkill(foundSkill) + ": " + compactText(foundSkill.Description)
}

func identifySkillChange(previous Skill, current Skill) string {
	identity := markdown.CodeSpan(current.Name)
	if previous.Name != current.Name {
		identity = markdown.CodeSpan(previous.Name) + " is now " + identity
	}
	return identity + " at " + markdown.CodeSpan(current.Location)
}

func describeSkillChange(previous Skill, current Skill) string {
	return identifySkillChange(previous, current) + ": " + compactText(current.Description)
}

func compactText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

const Change agent.Kind = "environment_change"

type eventState struct {
	KnownEnvironment Snapshot `json:"known"`
	Current          Snapshot `json:"current"`
}

func ChangeEvent(knownEnvironment Snapshot, current Snapshot) (agent.Event, error) {
	state, err := json.Marshal(eventState{
		KnownEnvironment: canonicalSnapshot(knownEnvironment),
		Current:          canonicalSnapshot(current),
	})
	if err != nil {
		return agent.Event{}, err
	}
	return agent.Event{Kind: Change, State: state}, nil
}

func decodeEvent(event agent.Event) (eventState, error) {
	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return eventState{}, err
	}
	state.KnownEnvironment = canonicalSnapshot(state.KnownEnvironment)
	state.Current = canonicalSnapshot(state.Current)
	return state, nil
}

func LastRecorded(events []agent.Event) (Snapshot, bool) {
	return access.LastRecorded(events, Change, func(event agent.Event) (Snapshot, error) {
		state, err := decodeEvent(event)
		return state.Current, err
	})
}

func Notice(event agent.Event) ([]string, bool) {
	return notices(event, userSkillWording)
}

const modelIntroduction = "The session environment changed since this conversation last ran. This is what changed:"

func ModelNotice(event agent.Event) ([]string, bool) {
	modelNotices, isSaid := notices(event, modelSkillWording)
	if !isSaid {
		return nil, false
	}
	return append([]string{modelIntroduction}, modelNotices...), true
}

func notices(event agent.Event, wording skillWording) ([]string, bool) {
	if event.Kind != Change {
		return nil, false
	}
	state, err := decodeEvent(event)
	if err != nil {
		return nil, false
	}
	changeDescriptions := changeNotices(state.KnownEnvironment, state.Current, wording)
	return changeDescriptions, len(changeDescriptions) > 0
}

func sandboxEqual(left Sandbox, right Sandbox) bool {
	return slices.Equal(left.DenyPatterns, right.DenyPatterns) &&
		slices.Equal(left.ReadPaths, right.ReadPaths) &&
		slices.Equal(left.WritePaths, right.WritePaths) &&
		slices.Equal(left.ExecutablePaths, right.ExecutablePaths) &&
		slices.Equal(left.PathDirectories, right.PathDirectories) &&
		slices.Equal(left.HomePaths, right.HomePaths)
}

func Summary(event agent.Event) (string, bool) {
	if event.Kind != Change {
		return "", false
	}
	state, err := decodeEvent(event)
	if err != nil {
		return "", false
	}

	var subjects []string
	if !sandboxEqual(state.KnownEnvironment.Sandbox, state.Current.Sandbox) {
		subjects = append(subjects, "sandbox")
	}
	if !slices.Equal(state.KnownEnvironment.Skills, state.Current.Skills) {
		subjects = append(subjects, "skills")
	}
	if state.KnownEnvironment.IsRepository != state.Current.IsRepository {
		subjects = append(subjects, "repository")
	}
	if state.KnownEnvironment.PortHostname != state.Current.PortHostname {
		subjects = append(subjects, "port hostname")
	}
	return strings.Join(subjects, ", "), len(subjects) > 0
}
