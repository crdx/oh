package harness

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/pkg/agent"
)

func TestStaticContextSourcesAccountForEveryPromptContribution(t *testing.T) {
	systemBody := strings.Repeat("s", 2_800)
	projectBody := strings.Repeat("p", 1_400)
	harnessBody := strings.Repeat("h", 5_600)
	skills := []skill.Skill{
		{Name: "one", Description: "First skill.", Location: "/skills/one/SKILL.md"},
		{Name: "two", Description: "Second skill.", Location: "/skills/two/SKILL.md"},
	}
	skillCatalogue := skill.Context(skills)
	systemPrompt := harnessBody + systemBody + projectBody + skillCatalogue
	files := []prompt.File{
		{Name: "SYSTEM.md", Path: "/config/SYSTEM.md", Body: systemBody, IsSystem: true},
		{Name: "AGENTS.md", Path: "/workspace/AGENTS.md", Body: projectBody},
	}

	got := identifyStaticContextSources(systemPrompt, files, skills)
	wantSystemFiles := []contextsource.Source{contextsource.FileFromBytes("/config/SYSTEM.md", len(systemBody))}
	wantProjectFiles := []contextsource.Source{contextsource.FileFromBytes("/workspace/AGENTS.md", len(projectBody))}
	wantSystemSources := []contextsource.Source{contextsource.KindFromBytes(contextsource.HarnessInstructions, len(harnessBody))}
	wantSessionSources := []contextsource.Source{
		contextsource.CountedFromBytes(contextsource.SkillDefinitions, 2, len(skillCatalogue)),
	}

	if !slices.Equal(got.systemFiles, wantSystemFiles) {
		t.Errorf("system files = %v, want %v", got.systemFiles, wantSystemFiles)
	}
	if !slices.Equal(got.projectFiles, wantProjectFiles) {
		t.Errorf("project files = %v, want %v", got.projectFiles, wantProjectFiles)
	}
	if !slices.Equal(got.systemSources, wantSystemSources) {
		t.Errorf("system sources = %v, want %v", got.systemSources, wantSystemSources)
	}
	if !slices.Equal(got.sessionSources, wantSessionSources) {
		t.Errorf("session sources = %v, want %v", got.sessionSources, wantSessionSources)
	}
}

func TestStaticContextWithNoFilesOrSkillsIsEntirelyHarness(t *testing.T) {
	const systemPrompt = "generated context"

	got := identifyStaticContextSources(systemPrompt, nil, nil)
	want := []contextsource.Source{contextsource.KindFromBytes(contextsource.HarnessInstructions, len(systemPrompt))}
	if !slices.Equal(got.systemSources, want) {
		t.Errorf("system sources = %v, want %v", got.systemSources, want)
	}
	if len(got.systemFiles) != 0 || len(got.projectFiles) != 0 || len(got.sessionSources) != 0 {
		t.Errorf("unexpected non-harness sources: %+v", got)
	}
}

func TestCurrentContextSourcesCombineEveryKindWithoutChangingTheFixedSources(t *testing.T) {
	staticSources := staticContextSources{
		systemFiles:    []contextsource.Source{{Path: "/config/SYSTEM.md", EstimatedTokens: 700}},
		projectFiles:   []contextsource.Source{{Path: "/workspace/AGENTS.md", EstimatedTokens: 400}},
		systemSources:  []contextsource.Source{{Kind: contextsource.HarnessInstructions, EstimatedTokens: 2_000}},
		sessionSources: []contextsource.Source{{Kind: contextsource.SkillDefinitions, Count: 1, EstimatedTokens: 300}},
	}
	toolSource := contextsource.Source{Kind: contextsource.ToolDefinitions, Count: 3, EstimatedTokens: 500}
	events := []agent.Event{
		{
			Kind: agent.ToolCallRequestEvent, ID: "skill", Name: "read",
			FallbackRendering: agent.FallbackRendering{Subject: "/skills/review/SKILL.md"},
		},
		{
			Kind: agent.ToolCallResultEvent, ID: "skill", Name: "read",
			Status: agent.SuccessStatus, Text: strings.Repeat("s", 1_400),
		},
	}

	got := currentContextSources(staticSources, &toolSource, events)
	want := commands.ContextSources{
		SystemSources: []contextsource.Source{
			{Kind: contextsource.HarnessInstructions, EstimatedTokens: 2_000},
			{Path: "/config/SYSTEM.md", EstimatedTokens: 700},
			{Kind: contextsource.ToolDefinitions, Count: 3, EstimatedTokens: 500},
		},
		ProjectSources: []contextsource.Source{{Path: "/workspace/AGENTS.md", EstimatedTokens: 400}},
		SessionSources: []contextsource.Source{
			{Kind: contextsource.SkillDefinitions, Count: 1, EstimatedTokens: 300},
			{Path: "/skills/review/SKILL.md", EstimatedTokens: 501},
		},
	}

	if !slices.Equal(got.SystemSources, want.SystemSources) ||
		!slices.Equal(got.ProjectSources, want.ProjectSources) ||
		!slices.Equal(got.SessionSources, want.SessionSources) {
		t.Errorf("context sources = %+v, want %+v", got, want)
	}
	if len(staticSources.systemSources) != 1 || len(staticSources.sessionSources) != 1 {
		t.Errorf("static context sources were changed: %+v", staticSources)
	}
}
