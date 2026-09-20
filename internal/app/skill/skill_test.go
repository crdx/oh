package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
)

func writeSkill(t *testing.T, skillsDirectory string, directory string, body string) string {
	t.Helper()

	path := filepath.Join(skillsDirectory, directory, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func projectSkills(project string) string {
	return filepath.Join(project, ".agents", "skills")
}

func TestDiscoverReadsProjectAndGlobalDirectories(t *testing.T) {
	project := t.TempDir()
	globalDirectory := t.TempDir()
	projectPath := writeSkill(t, projectSkills(project), "review", "---\nname: review\ndescription: Review this project.\n---\nBody")
	writeSkill(t, globalDirectory, "review", "---\nname: review\ndescription: Review anything.\n---\nBody")
	globalPath := writeSkill(t, globalDirectory, "pdf", "---\nname: pdf\ndescription: Work with PDFs.\n---\nBody")

	discoveredSkills, err := Discover(project, []string{globalDirectory}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discoveredSkills) != 3 {
		t.Fatalf("got %d skills, want 3: %#v", len(discoveredSkills), discoveredSkills)
	}
	if discoveredSkills[0].Description != "Review this project." || discoveredSkills[0].Location != projectPath {
		t.Errorf("got project skill %#v", discoveredSkills[0])
	}
	if discoveredSkills[1].Location != globalPath {
		t.Errorf("got global location %q, want %q", discoveredSkills[1].Location, globalPath)
	}
	projectCount, globalCount := Counts(discoveredSkills)
	if projectCount != 1 || globalCount != 2 {
		t.Errorf("got %d project and %d global skills, want 1 and 2", projectCount, globalCount)
	}
}

func TestExcludeGlobalMatchesAbsoluteDirectories(t *testing.T) {
	project := t.TempDir()
	globalDirectory := t.TempDir()
	writeSkill(t, projectSkills(project), "review", "---\nname: review\ndescription: Review this project.\n---\nBody")
	globalReview := writeSkill(t, globalDirectory, "review", "---\nname: review\ndescription: Review anything.\n---\nBody")
	writeSkill(t, globalDirectory, "pi", "---\nname: pi\ndescription: Work on pi.\n---\nBody")

	discoveredSkills, err := Discover(project, []string{globalDirectory}, nil)
	if err != nil {
		t.Fatal(err)
	}
	filteredSkills := ExcludeGlobal(discoveredSkills, []string{filepath.Dir(globalReview)})
	if len(filteredSkills) != 2 {
		t.Fatalf("got %#v, want two skills", filteredSkills)
	}
	projectReview := filteredSkills[0]
	globalPi := filteredSkills[1]
	if projectReview.Name != "review" || projectReview.isGlobal || globalPi.Name != "pi" || !globalPi.isGlobal {
		t.Errorf("got %#v, want the project review and global pi skills", filteredSkills)
	}
}

func TestDiscoverReadsEveryAdditionalDirectoryOnce(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeSkill(t, first, "first", "---\ndescription: First.\n---\nBody")
	writeSkill(t, second, "second", "---\ndescription: Second.\n---\nBody")

	discoveredSkills, err := Discover(t.TempDir(), []string{first, second, first}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discoveredSkills) != 2 {
		t.Fatalf("got %d skills, want 2: %#v", len(discoveredSkills), discoveredSkills)
	}
	if discoveredSkills[0].Name != "first" || discoveredSkills[1].Name != "second" {
		t.Errorf("got skills %#v", discoveredSkills)
	}
}

func TestMalformedSkillsAreWarnedAboutAndSkipped(t *testing.T) {
	project := t.TempDir()
	writeSkill(t, projectSkills(project), "broken", "not frontmatter")
	writeSkill(t, projectSkills(project), "empty", "---\nname: empty\n---\nBody")
	writeSkill(t, projectSkills(project), "actual-directory", "---\nname: other-name\ndescription: Still useful.\n---\nBody")

	var warnings strings.Builder
	discoveredSkills, err := Discover(project, nil, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if len(discoveredSkills) != 1 || discoveredSkills[0].Name != "other-name" {
		t.Fatalf("got %#v, want the one usable skill", discoveredSkills)
	}
	for _, text := range []string{"broken", "description is missing", "does not match directory"} {
		if !strings.Contains(warnings.String(), text) {
			t.Errorf("warning does not contain %q: %q", text, warnings.String())
		}
	}
}

func TestAMissingNameFallsBackToTheDirectory(t *testing.T) {
	project := t.TempDir()
	writeSkill(t, projectSkills(project), "fallback", "---\ndescription: Has no explicit name.\n---\nBody")

	discoveredSkills, err := Discover(project, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discoveredSkills) != 1 || discoveredSkills[0].Name != "fallback" {
		t.Errorf("got %#v", discoveredSkills)
	}
}

func TestASkillIsRecognisedByThePathItLivesAt(t *testing.T) {
	tests := map[string]struct {
		path string
		name string
		want bool
	}{
		"absolute":            {path: "/skills/golang/SKILL.md", name: "golang", want: true},
		"project":             {path: ".agents/skills/oh-config/SKILL.md", name: "oh-config", want: true},
		"relative":            {path: "skills/guard-basics/SKILL.md", name: "guard-basics", want: true},
		"another file":        {path: "cmd/oh/draw.go"},
		"another parent":      {path: "docs/golang/SKILL.md"},
		"no skill of its own": {path: "skills/SKILL.md"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			skillName, isSkill := NameFromPath(test.path)
			if skillName != test.name || isSkill != test.want {
				t.Errorf(
					"got name %q and %v, want name %q and %v",
					skillName, isSkill, test.name, test.want,
				)
			}
		})
	}
}

func TestPromptDisclosesOnlyTheCatalogueAndEscapesXML(t *testing.T) {
	got := Context([]Skill{{
		Name:        "one&two",
		Description: "Use <carefully>.",
		Location:    "/skills/one/SKILL.md",
	}})

	for _, want := range []string{
		"<available_skills>", "<name>one&amp;two</name>",
		"<description>Use &lt;carefully&gt;.</description>", "use the read tool",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt does not contain %q: %q", want, got)
		}
	}
	if strings.Contains(got, "skill body") {
		t.Errorf("catalogue unexpectedly contains a body: %q", got)
	}
	if strings.Contains(got, "<root>") {
		t.Errorf("catalogue is named after the struct rather than the element: %q", got)
	}

	opening := strings.Index(got, "<available_skills>")
	entry := strings.Index(got, "<skill>")
	closing := strings.Index(got, "</available_skills>")
	if opening < 0 || entry < opening || closing < entry {
		t.Errorf("skills are not nested inside the catalogue: %q", got)
	}
	if empty := Context(nil); empty != "" {
		t.Errorf("empty catalogue got %q", empty)
	}
}

func TestAGlobalSkillMountCanBeReadButNotWritten(t *testing.T) {
	workspace := t.TempDir()
	globalDirectory := t.TempDir()
	location := writeSkill(t, globalDirectory, "mounted", "---\nname: mounted\ndescription: Mounted.\n---\nBody")

	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()
	files := file.New(workspaceRoot, func(string) error { return nil })

	discoveredSkills, err := Discover(workspace, []string{globalDirectory}, nil)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := MountGlobalSkills(files, discoveredSkills)
	if err != nil {
		t.Fatal(err)
	}
	defer Close(roots)

	resolvedRoot, name, err := files.Resolve(location)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := resolvedRoot.ReadFile(name); err != nil || !strings.Contains(string(data), "Body") {
		t.Errorf("got %q and %v", data, err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("got write error %v, want read-only", err)
	}
}
