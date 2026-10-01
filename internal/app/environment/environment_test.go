package environment

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/pkg/agent"
)

func TestCaptureClonesAndCanonicalisesTheEnvironment(t *testing.T) {
	paths := shell.Paths{
		Deny: []string{"two", "one", "one"},
		Path: []string{"/second", "/first", "/second"},
	}
	skills := []skill.Skill{{Name: "write", Description: "write things", Location: "/skills/write/SKILL.md"}}

	captured := Capture(paths, skills, true, "session.test")
	paths.Deny[0] = "changed"
	skills[0].Name = "changed"

	if !slices.Equal(captured.Sandbox.DenyPatterns, []string{"one", "two"}) {
		t.Errorf("got deny patterns %q", captured.Sandbox.DenyPatterns)
	}
	if !slices.Equal(captured.Sandbox.PathDirectories, []string{"/second", "/first"}) {
		t.Errorf("got PATH directories %q", captured.Sandbox.PathDirectories)
	}
	if captured.Skills[0].Name != "write" {
		t.Errorf("captured skill changed to %q", captured.Skills[0].Name)
	}
}

func TestEveryEnvironmentChangeIsDescribed(t *testing.T) {
	known := Snapshot{
		Sandbox: Sandbox{
			DenyPatterns:    []string{"old-secret"},
			ReadPaths:       []string{"/old-read"},
			WritePaths:      []string{"/old-write"},
			ExecutablePaths: []string{"/old-executable"},
			PathDirectories: []string{"/old-path"},
			HomePaths:       []string{"/old-home"},
		},
		Skills: []Skill{
			{Name: "removed", Description: "old", Location: "/skills/removed/SKILL.md"},
			{Name: "changed", Description: "old description", Location: "/skills/changed/SKILL.md"},
			{Name: "renamed-old", Description: "renamed description", Location: "/skills/renamed/SKILL.md"},
			{Name: "moved", Description: "moved description", Location: "/skills/moved-old/SKILL.md"},
		},
		PortHostname: "old.test",
	}
	current := Snapshot{
		Sandbox: Sandbox{
			DenyPatterns:    []string{"new-secret"},
			ReadPaths:       []string{"/new-read"},
			WritePaths:      []string{"/new-write"},
			ExecutablePaths: []string{"/new-executable"},
			PathDirectories: []string{"/new-path", "/second-path"},
			HomePaths:       []string{"/new-home"},
		},
		Skills: []Skill{
			{Name: "added", Description: "new skill", Location: "/skills/added/SKILL.md"},
			{Name: "changed", Description: "new description", Location: "/skills/changed/SKILL.md"},
			{Name: "renamed-new", Description: "renamed description", Location: "/skills/renamed/SKILL.md"},
			{Name: "moved", Description: "moved description", Location: "/skills/moved-new/SKILL.md"},
		},
		IsRepository: true,
		PortHostname: "new.test",
	}

	event, err := ChangeEvent(known, current)
	if err != nil {
		t.Fatal(err)
	}
	notices, areSaid := Notice(event)
	if !areSaid {
		t.Fatal("the changed environment said nothing")
	}
	joined := strings.Join(notices, "\n")
	if strings.Contains(joined, "session environment changed") {
		t.Errorf("the user-facing notice contains the model introduction:\n%s", joined)
	}
	modelNotices, isModelTold := ModelNotice(event)
	if !isModelTold || !strings.HasPrefix(modelNotices[0], "The session environment changed since this conversation last ran") {
		t.Errorf("got model notices %q and told %v", modelNotices, isModelTold)
	}
	joinedModelNotices := strings.Join(modelNotices, "\n")
	for _, description := range []string{"new skill", "new description"} {
		if !strings.Contains(joinedModelNotices, description) {
			t.Errorf("model notice omits the skill description %q:\n%s", description, joinedModelNotices)
		}
	}
	for _, description := range []string{"new skill", "new description", "renamed description"} {
		if strings.Contains(joined, description) {
			t.Errorf("user-facing notice contains the skill description %q:\n%s", description, joined)
		}
	}
	for _, wanted := range []string{
		"new-secret", "old-secret", "/new-read", "/old-read", "/new-write", "/old-write",
		"/new-executable", "/old-executable", "/new-path", "/second-path", "/new-home", "/old-home",
		"Skills now available", "`added`", "/skills/moved-new/SKILL.md",
		"Skills no longer available", "`removed`", "/skills/moved-old/SKILL.md",
		"Skills changed", "`renamed-old` is now `renamed-new`",
		"now a Git repository", "`new.test`", "`old.test`",
	} {
		if !strings.Contains(joined, wanted) {
			t.Errorf("notice does not contain %q:\n%s", wanted, joined)
		}
	}

	summary, isSummarised := Summary(event)
	if !isSummarised || summary != "sandbox, skills, repository, port hostname" {
		t.Errorf("got summary %q and summarised %v", summary, isSummarised)
	}
}

func TestARecordedEnvironmentChangeIsNotQueuedAgain(t *testing.T) {
	created := Snapshot{PortHostname: "old.test"}
	current := Snapshot{PortHostname: "new.test"}
	first, err := Restore(&created, nil, current)
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsChanged {
		t.Fatal("the first restoration did not report the hostname change")
	}

	second, err := Restore(&created, []agent.Event{first.Change}, current)
	if err != nil {
		t.Fatal(err)
	}
	if second.IsChanged {
		t.Error("the recorded environment change was queued again")
	}
	if notice := second.State.Peek(); notice != "" {
		t.Errorf("the recorded environment change still says %q", notice)
	}
}

func TestARecoveredEnvironmentIsComparedWithTheLastRecordedChange(t *testing.T) {
	created := Snapshot{PortHostname: "old.test"}
	changedEnvironment := Snapshot{IsRepository: true, PortHostname: "new.test"}
	changed, err := Restore(&created, nil, changedEnvironment)
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := Restore(&created, []agent.Event{changed.Change}, created)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.IsChanged {
		t.Fatal("the recovered environment was not reported")
	}
	notices, areSaid := Notice(recovered.Change)
	joined := strings.Join(notices, "\n")
	for _, wanted := range []string{"no longer a Git repository", "now use `old.test` instead of `new.test`"} {
		if !areSaid || !strings.Contains(joined, wanted) {
			t.Errorf("got notice %q and said %v, want %q", joined, areSaid, wanted)
		}
	}
}

func TestALegacySessionWithoutAnEnvironmentSnapshotSaysNothing(t *testing.T) {
	current := Snapshot{
		Sandbox:      Sandbox{ReadPaths: []string{"/reference"}},
		Skills:       []Skill{{Name: "review", Description: "review things", Location: "/skills/review/SKILL.md"}},
		IsRepository: true,
		PortHostname: "session.test",
	}

	restored, err := Restore(nil, nil, current)
	if err != nil {
		t.Fatal(err)
	}
	if restored.IsChanged {
		t.Error("a legacy session invented an earlier environment")
	}
	if !restored.NeedsBaseline {
		t.Error("a legacy session did not request a baseline record")
	}
}

func TestManyEnvironmentChangesAreNotTruncated(t *testing.T) {
	current := Snapshot{}
	for index := range 100 {
		label := fmt.Sprintf("item-%03d", index)
		current.Sandbox.ReadPaths = append(current.Sandbox.ReadPaths, "/"+label)
		current.Skills = append(current.Skills, Skill{
			Name:        label,
			Description: "use " + label,
			Location:    "/skills/" + label + "/SKILL.md",
		})
	}

	event, err := ChangeEvent(Snapshot{}, current)
	if err != nil {
		t.Fatal(err)
	}
	notices, areSaid := Notice(event)
	if !areSaid {
		t.Fatal("the large environment change said nothing")
	}
	joined := strings.Join(notices, "\n")
	for index := range 100 {
		label := fmt.Sprintf("item-%03d", index)
		if !strings.Contains(joined, "/"+label) || !strings.Contains(joined, "`"+label+"`") {
			t.Errorf("the large environment change omitted %s", label)
		}
	}
}
