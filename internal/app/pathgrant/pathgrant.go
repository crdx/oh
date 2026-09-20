package pathgrant

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"crdx.org/io/internal/app/access"
	"crdx.org/io/internal/app/shell"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/pkg/agent"
)

type Access = shell.Access

const (
	ReadAccess  = shell.ReadAccess
	ExecAccess  = shell.ExecAccess
	WriteAccess = shell.WriteAccess
)

type Grant struct {
	Path   string
	Access Access
}

type RestoreFailure struct {
	Grant Grant
	Err   error
}

type RestoreResult struct {
	Failures []RestoreFailure
}

type Grants struct {
	workspace  *work.Space
	pathAccess *shell.PathAccess
	state      *access.State[[]Grant]
	mutex      sync.Mutex
}

func New(workspace *work.Space, pathAccess *shell.PathAccess) *Grants {
	return &Grants{
		workspace:  workspace,
		pathAccess: pathAccess,
		state:      access.New([]Grant(nil), definition()),
	}
}

func NewRestored(
	workspace *work.Space,
	pathAccess *shell.PathAccess,
	recordedGrants []Grant,
) (*Grants, RestoreResult) {
	current := make([]Grant, 0, len(recordedGrants))
	result := RestoreResult{}
	for _, grant := range canonicalGrants(recordedGrants) {
		var err error
		if !filepath.IsAbs(grant.Path) || filepath.Clean(grant.Path) != grant.Path {
			err = fmt.Errorf("invalid recorded path %q", grant.Path)
		} else if !shell.IsAccess(grant.Access) {
			err = fmt.Errorf("invalid recorded access %q", grant.Access.Flags())
		}
		canonicalPath := ""
		if err == nil {
			canonicalPath, err = filepath.EvalSymlinks(grant.Path)
		}
		if err == nil && filepath.Clean(canonicalPath) != grant.Path {
			err = fmt.Errorf("path now resolves to %s", filepath.Clean(canonicalPath))
		}
		if err == nil {
			_, err = pathAccess.Grant(grant.Path, grant.Access)
		}
		if err != nil {
			result.Failures = append(result.Failures, RestoreFailure{Grant: grant, Err: err})
			continue
		}
		current = append(current, grant)
	}

	return &Grants{
		workspace:  workspace,
		pathAccess: pathAccess,
		state:      access.NewRestored(current, canonicalGrants(recordedGrants), definition()),
	}, result
}

func (self *Grants) GetCurrent() []Grant {
	return self.state.GetCurrent()
}

func (self *Grants) IsTold(path string) bool {
	knownGrant, isKnown := findGrant(self.state.GetKnown(), path)
	currentGrant, isCurrent := findGrant(self.state.GetCurrent(), path)

	return isKnown == isCurrent && knownGrant.Access == currentGrant.Access
}

func (self *Grants) Peek() string {
	return self.state.Peek()
}

func (self *Grants) Inject() string {
	return self.state.Inject()
}

func (self *Grants) Grant(path string, access Access) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if !shell.IsAccess(access) {
		return agent.Event{}, fmt.Errorf(
			"access is %q, want some of %q", access.Flags(), shell.AllAccessFlags,
		)
	}
	canonicalPath, err := self.canonicalPath(path, true)
	if err != nil {
		return agent.Event{}, err
	}

	current := self.state.GetCurrent()
	if existingGrant, found := findGrant(current, canonicalPath); found && existingGrant.Access == access {
		return agent.Event{}, fmt.Errorf(
			"%s already has temporary %s access", pathutil.Shorten(canonicalPath), access.Describe(),
		)
	}
	if _, err := self.pathAccess.Grant(canonicalPath, access); err != nil {
		return agent.Event{}, fmt.Errorf("could not grant access to %s: %w", pathutil.Shorten(canonicalPath), err)
	}

	current = replaceGrant(current, Grant{Path: canonicalPath, Access: access})
	self.state.Replace(current)
	return ChangeEvent(canonicalPath, current)
}

func (self *Grants) Revoke(path string) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	canonicalPath, err := self.canonicalPath(path, false)
	if err != nil {
		return agent.Event{}, err
	}
	current := self.state.GetCurrent()
	if _, found := findGrant(current, canonicalPath); !found {
		return agent.Event{}, fmt.Errorf("%s has no temporary grant", pathutil.Shorten(canonicalPath))
	}
	if !self.pathAccess.Revoke(canonicalPath) {
		return agent.Event{}, errors.New("temporary path access was lost before it could be revoked")
	}

	current = slices.DeleteFunc(current, func(grant Grant) bool { return grant.Path == canonicalPath })
	self.state.Replace(current)
	return ChangeEvent(canonicalPath, current)
}

func (self *Grants) canonicalPath(path string, mustExist bool) (string, error) {
	writtenPath := strings.TrimSpace(path)
	if writtenPath == "" {
		return "", errors.New("path is empty")
	}
	path, err := pathutil.Expand(writtenPath)
	if err != nil {
		return "", fmt.Errorf("could not expand %s: %w", writtenPath, err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(self.workspace.GetResolvedDir(), path)
	}
	path = filepath.Clean(path)

	canonicalPath, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(canonicalPath), nil
	}
	if mustExist {
		return "", fmt.Errorf("could not resolve %s: %w", pathutil.Shorten(path), err)
	}
	return path, nil
}

func replaceGrant(grants []Grant, replacement Grant) []Grant {
	isReplaced := false
	updatedGrants := make([]Grant, 0, len(grants)+1)
	for _, grant := range grants {
		if grant.Path == replacement.Path {
			updatedGrants = append(updatedGrants, replacement)
			isReplaced = true
		} else {
			updatedGrants = append(updatedGrants, grant)
		}
	}
	if !isReplaced {
		updatedGrants = append(updatedGrants, replacement)
	}
	return canonicalGrants(updatedGrants)
}

func findGrant(grants []Grant, path string) (Grant, bool) {
	for _, grant := range grants {
		if grant.Path == path {
			return grant, true
		}
	}
	return Grant{}, false
}

func canonicalGrants(grants []Grant) []Grant {
	grantsByPath := make(map[string]Grant, len(grants))
	for _, grant := range grants {
		grantsByPath[grant.Path] = grant
	}

	paths := slices.Sorted(maps.Keys(grantsByPath))
	grantList := make([]Grant, 0, len(paths))
	for _, path := range paths {
		grantList = append(grantList, grantsByPath[path])
	}
	return grantList
}

func definition() access.Definition[[]Grant] {
	return access.Definition[[]Grant]{
		Clone:    canonicalGrants,
		Describe: describeChanges,
	}
}

func describeChanges(knownGrants []Grant, currentGrants []Grant) string {
	var clauses []string
	for _, grant := range currentGrants {
		previous, found := findGrant(knownGrants, grant.Path)
		if found && previous.Access == grant.Access {
			continue
		}
		clauses = append(clauses, grantNotice(grant))
	}
	for _, grant := range knownGrants {
		if _, found := findGrant(currentGrants, grant.Path); !found {
			clauses = append(clauses, revocationNotice(grant.Path))
		}
	}
	return strings.Join(clauses, " ")
}

const Change agent.Kind = "path_grant_change"

type eventState struct {
	Grants []writtenGrant `json:"grants"`
}

type writtenGrant struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

func ChangeEvent(path string, grants []Grant) (agent.Event, error) {
	writtenGrants := make([]writtenGrant, 0, len(grants))
	for _, grant := range canonicalGrants(grants) {
		writtenGrants = append(writtenGrants, writtenGrant{
			Path:   grant.Path,
			Access: grant.Access.Flags(),
		})
	}

	state, err := json.Marshal(eventState{Grants: writtenGrants})
	if err != nil {
		return agent.Event{}, err
	}
	return agent.Event{Kind: Change, Name: path, State: state}, nil
}

func WithGrantOf(event agent.Event, path string, grants []Grant) agent.Event {
	state, err := decodeEvent(event)
	if err != nil {
		return event
	}

	state = slices.DeleteFunc(state, func(grant Grant) bool { return grant.Path == path })
	if grant, found := findGrant(grants, path); found {
		state = append(state, grant)
	}

	restatedEvent, err := ChangeEvent(event.Name, state)
	if err != nil {
		return event
	}

	return restatedEvent
}

func decodeEvent(event agent.Event) ([]Grant, error) {
	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil, err
	}

	grants := make([]Grant, 0, len(state.Grants))
	for _, entry := range state.Grants {
		grantedAccess, err := shell.ParseAccess(entry.Access)
		if err != nil || !filepath.IsAbs(entry.Path) || filepath.Clean(entry.Path) != entry.Path ||
			!shell.IsAccess(grantedAccess) {
			return nil, errors.New("invalid path grant state")
		}
		grants = append(grants, Grant{Path: entry.Path, Access: grantedAccess})
	}

	return canonicalGrants(grants), nil
}

func LastRecorded(events []agent.Event) ([]Grant, bool) {
	return access.LastRecorded(events, Change, decodeEvent)
}

func Summary(event agent.Event) (string, bool) {
	if event.Kind != Change {
		return "", false
	}
	grants, err := decodeEvent(event)
	if err != nil {
		return "", false
	}
	if len(grants) == 0 {
		return "none", true
	}
	if len(grants) == 1 {
		return "1 path", true
	}
	return fmt.Sprintf("%d paths", len(grants)), true
}

func Notice(event agent.Event) (string, bool) {
	if event.Kind != Change || event.Name == "" {
		return "", false
	}
	grants, err := decodeEvent(event)
	if err != nil {
		return "", false
	}
	if grant, found := findGrant(grants, event.Name); found {
		return grantNotice(grant), true
	}
	return revocationNotice(event.Name), true
}

func revocationNotice(path string) string {
	return "Revoked temporary access to " + path + "."
}

func grantNotice(grant Grant) string {
	return "Granted temporary " + grant.Access.Describe() + " access to " +
		grant.Path + "." + capabilityClauses(grant.Access)
}

func capabilityClauses(access Access) string {
	clauses := ""
	if access.Has(ExecAccess) {
		clauses += " Execution there follows the shell capability."
	}
	if access.Has(WriteAccess) {
		clauses += " Changes there follow the workspace write capability."
	}

	return clauses
}
