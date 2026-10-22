package harness

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/tool"
)

type childWorkspaceFixture struct {
	workspace string
	scratch   string
	outside   string
	resolve   func(string) (string, error)
}

func newChildWorkspaceFixture(t *testing.T, isParentUnconfined bool) childWorkspaceFixture {
	t.Helper()

	fixture := childWorkspaceFixture{
		workspace: reachableWorkspaceDir(t),
		scratch:   reachableWorkspaceDir(t),
		outside:   reachableWorkspaceDir(t),
	}
	for _, directory := range []string{
		filepath.Join(fixture.workspace, "docs"),
		filepath.Join(fixture.scratch, "job"),
	} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fixture.workspace, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(fixture.scratch, "docs"):    filepath.Join(fixture.workspace, "docs"),
		filepath.Join(fixture.scratch, "outside"): fixture.outside,
		filepath.Join(fixture.scratch, "host"):    os.TempDir(),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}

	workspace := work.At(fixture.workspace)
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	files := file.New(workspace.GetRoot(), func(string) error { return file.ErrReadOnly })
	shell.MountScratch(files, fixture.scratch, file.NewLazy(fixture.scratch, allowWrite), false)
	fixture.resolve = childWorkspace(childOptions{workspace: workspace, parentFiles: files, isParentUnconfined: isParentUnconfined})
	return fixture
}

func TestAChildWorksWhereverItsParentCanRead(t *testing.T) {
	fixture := newChildWorkspaceFixture(t, false)
	for directory, want := range map[string]string{
		"":                fixture.workspace,
		"docs":            filepath.Join(fixture.workspace, "docs"),
		fixture.workspace: fixture.workspace,
		"/tmp/job":        filepath.Join(fixture.scratch, "job"),
	} {
		got, err := fixture.resolve(directory)
		if err != nil || got != want {
			t.Errorf("%q resolved to %q, %v; want %q", directory, got, err, want)
		}
	}
}

func TestAChildNeverWorksWhereItsParentCannotRead(t *testing.T) {
	fixture := newChildWorkspaceFixture(t, false)
	for directory, refusal := range map[string]string{
		"README.md":     "is not a directory",
		"/tmp/missing":  "is not a directory you can read",
		fixture.outside: "is not a directory you can read",
		"/tmp/outside":  "is not a directory you can read",
		"/tmp/host":     "is not a directory you can read",
		"/tmp/docs":     "is not a directory you can read",
		"../escaped":    "is not a directory you can read",
	} {
		if got, err := fixture.resolve(directory); err == nil || !strings.Contains(err.Error(), refusal) {
			t.Errorf("%q resolved to %q, %v; want it refused as %q", directory, got, err, refusal)
		}
	}
}

func TestAChildOfAnUnconfinedParentWorksInAnyHostDirectoryOutsideHostTmp(t *testing.T) {
	fixture := newChildWorkspaceFixture(t, true)
	if got, err := fixture.resolve(fixture.outside); err != nil || got != fixture.outside {
		t.Errorf("a child of an unconfined parent could not work in %s: %q, %v", fixture.outside, got, err)
	}
	if _, err := fixture.resolve(filepath.Join(fixture.workspace, "README.md")); err == nil {
		t.Error("a child of an unconfined parent was given a file as its workspace")
	}
	hostDirectory := hostTmpRigFixture(t)
	if got, err := fixture.resolve(hostDirectory); err == nil || !strings.Contains(err.Error(), "leads into the host's /tmp") {
		t.Errorf("a child of an unconfined parent was given %q, %v as its workspace in the host's /tmp", got, err)
	}
}

var childPromptSettings = config.Config{Sandbox: shell.Paths{
	Read: []string{"/srv/docs"},
	Home: []string{"/users/person/.local/share/notes", "/etc/person"},
	Exec: []string{"/opt"},
	Path: []string{"/users/person/bin"},
	Deny: []string{".env"},
}}

func TestGoldenAChildIsToldWhatItCanDo(t *testing.T) {
	configDirectory := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDirectory)
	if err := os.MkdirAll(filepath.Join(configDirectory, "org.crdx", "oh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, "org.crdx", "oh", "SYSTEM.md"), []byte("Global rules."), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaceDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDirectory, "AGENTS.md"), []byte("Workspace rules."), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := work.At(workspaceDirectory)
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	globalSkills := filepath.Join(configDirectory, "skills")
	for directory, description := range map[string]string{
		filepath.Join(globalSkills, "probe"):                                "Probe a child. Use when probing.",
		filepath.Join(workspaceDirectory, ".agents", "skills", "workspace"): "Work in the workspace. Use when working.",
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		contents := "---\nname: " + filepath.Base(directory) + "\ndescription: " + description + "\n---\n\nBody.\n"
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	skills, err := discoverSkills(workspaceDirectory, []string{globalSkills}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	workspaceSkills := slices.DeleteFunc(slices.Clone(skills), func(found skill.Skill) bool { return found.Name != "workspace" })
	plainBrief := childBrief{
		dropsDirectory: "/state/sessions/tame-impala/subagents/tame-adder/drops",
		notes:          agent.SystemReminders,
	}
	skilledBrief, workspaceSkilledBrief, codexBrief := plainBrief, plainBrief, plainBrief
	skilledBrief.skills = skills
	workspaceSkilledBrief.skills = workspaceSkills
	codexBrief.notes = codex.Notes
	prompt := func(
		isParentUnconfined bool,
		childCaps caps.Set,
		sharedPrompt string,
		brief childBrief,
		options ...childOptions,
	) func() string {
		return func() string {
			child := subagents.Child{
				Name:         "tame-adder",
				Workspace:    workspaceDirectory,
				Caps:         childCaps,
				Scratch:      "/state/farm/tame-impala/subagents/tame-adder",
				SharedPrompt: sharedPrompt,
			}
			promptOptions := childOptions{userHome: "/users/person", settings: childPromptSettings, scratchParent: "/state/farm/tame-impala"}
			if len(options) > 0 {
				promptOptions = options[0]
			}
			promptOptions.sessionName, promptOptions.isParentUnconfined = goldenSessionName, isParentUnconfined
			drawn := childPrompt(
				promptOptions, workspace, child,
				"/state/farm/tame-impala/subagents/tame-adder/home", brief,
			)
			if strings.Contains(drawn, "Global rules.") || strings.Contains(drawn, "Workspace rules.") {
				t.Errorf("a child was handed its parent's context files: %q", drawn)
			}
			drawn = strings.ReplaceAll(drawn, configDirectory, "/config")
			return strings.ReplaceAll(drawn, workspaceDirectory, sessionGoldenWorkspace) + "\n"
		}
	}
	farmReadable := childOptions{
		userHome:      "/users/person",
		scratchParent: "/state/farm/tame-impala",
		settings:      config.Config{Sandbox: shell.Paths{Read: []string{"/state/farm"}}},
	}
	compareWithGolden(t, "subagent-prompt", ".txt", map[string]func() string{
		"confined with a shell":                                    prompt(false, caps.Read|caps.Shell, "", plainBrief),
		"confined without a shell":                                 prompt(false, caps.Read, "", plainBrief),
		"confined under an unconfined parent":                      prompt(true, caps.Read|caps.Shell, "", plainBrief),
		"with shared instructions":                                 prompt(false, caps.Read|caps.Shell, "Answer in one sentence.", plainBrief),
		"with nothing configured and no known home":                prompt(false, caps.Read|caps.Shell, "", plainBrief, childOptions{}),
		"with skills and a shell":                                  prompt(false, caps.Read|caps.Shell, "Use the skills.", skilledBrief),
		"with skills and no shell":                                 prompt(false, caps.Read, "Use the skills.", skilledBrief),
		"with only workspace skills and a shell":                   prompt(false, caps.Read|caps.Shell, "", workspaceSkilledBrief),
		"on a provider with its own note format":                   prompt(false, caps.Read|caps.Shell, "", codexBrief),
		"with its parent's scratch readable":                       prompt(false, caps.Read|caps.Shell, "", plainBrief, farmReadable),
		"with sibling scratch readable under an unconfined parent": prompt(true, caps.Read|caps.Shell, "", plainBrief, farmReadable),
	})
}

func stubTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: name, Description: name, Schema: tool.Schema{}},
		func(struct{}) tool.CallRendering { return tool.CallRendering{} },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil })
}

func TestAFollowUpKeepsTheToolsItsChildWasFirstOffered(t *testing.T) {
	current := []tool.Tool{stubTool("read"), stubTool("grep")}
	frozen := store.FreezeTools([]tool.Tool{stubTool("read"), stubTool("retired")})

	worker, changes, err := childAgent("prompt", unaskedProvider{}, current, subagents.Child{FrozenTools: frozen})
	if err != nil {
		t.Fatal(err)
	}
	if !worker.IsToolEnabled("retired") {
		t.Error("a tool the child was first offered is missing from its follow-up")
	}
	if _, isRunnable := worker.Tool("retired"); isRunnable {
		t.Error("a retired tool can still be run")
	}
	if worker.IsToolEnabled("grep") {
		t.Error("a follow-up was offered a tool its child never had")
	}
	if len(changes) != 1 {
		t.Fatalf("got %d availability changes, want the retired tool announced", len(changes))
	}
	if notices, isSaid := toolset.AvailabilityNotice(changes[0]); !isSaid || !strings.Contains(strings.Join(notices, " "), "retired") {
		t.Errorf("the child was told %q", notices)
	}

	unchanged, changes, err := childAgent("prompt", unaskedProvider{}, current, subagents.Child{FrozenTools: store.FreezeTools(current)})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("a follow-up with every tool intact announced %d changes", len(changes))
	}
	if !unchanged.IsToolEnabled("grep") {
		t.Error("an intact tool was dropped")
	}
	if fresh, changes, err := childAgent("prompt", unaskedProvider{}, current, subagents.Child{}); err != nil || len(changes) != 0 {
		t.Errorf("a new child got %d changes, %v", len(changes), err)
	} else if !fresh.IsToolEnabled("grep") {
		t.Error("a new child was not offered the current tools")
	}
}

func TestAChildsReportIsMarkedUnverifiedAndItsScratchNamedAsItsParentSeesIt(t *testing.T) {
	confined := childScratchNote(childOptions{scratchParent: "/state/farm/tame-impala"})
	if note := confined("tame-adder"); note != "unverified: check what matters before relying on it; its /tmp is your /tmp/subagents/tame-adder, so read any /tmp path it reports as beneath that" {
		t.Errorf("a confined parent was told %q", note)
	}
	unconfined := childScratchNote(childOptions{scratchParent: "/state/farm/tame-impala", isParentUnconfined: true})
	if note := unconfined("tame-adder"); note != "unverified: check what matters before relying on it; its /tmp is /state/farm/tame-impala/subagents/tame-adder, so read any /tmp path it reports as beneath that" {
		t.Errorf("an unconfined parent was told %q", note)
	}
}

func TestAChildIsGivenEveryReadablePathItsParentIsConfiguredWith(t *testing.T) {
	configured := shell.Paths{
		Deny:  []string{".env"},
		Read:  []string{"/srv/docs"},
		Write: []string{"/srv/out"},
		Exec:  []string{"/opt"},
		Path:  []string{"/users/person/bin"},
		Home:  []string{"/users/person/.config/git"},
	}
	got := childPaths(configured)
	want := configured
	want.Write = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a child was given %+v, want everything but the writable paths: %+v", got, want)
	}
	configured.Read = append(configured.Read, filepath.Join(sandbox.TmpDir, "notes"))
	if got := childPaths(configured); !reflect.DeepEqual(got, want) {
		t.Errorf("a child was given %+v, want the host's /tmp left out: %+v", got, want)
	}
}
