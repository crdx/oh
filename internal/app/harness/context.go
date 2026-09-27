package harness

import (
	"fmt"
	"slices"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
)

type staticContextSources struct {
	systemFiles    []contextsource.Source
	projectFiles   []contextsource.Source
	systemSources  []contextsource.Source
	sessionSources []contextsource.Source
}

func identifyStaticContextSources(
	systemPrompt string,
	files []prompt.File,
	skills []skill.Skill,
) staticContextSources {
	var sources staticContextSources
	contextFileBytes := 0
	for _, loadedFile := range files {
		loadedBytes := len(loadedFile.Body)
		contextFileBytes += loadedBytes
		contextFile := contextsource.FileFromBytes(loadedFile.Path, loadedBytes)
		if loadedFile.IsSystem {
			sources.systemFiles = append(sources.systemFiles, contextFile)
		} else {
			sources.projectFiles = append(sources.projectFiles, contextFile)
		}
	}

	skillCatalogue := skill.Context(skills)
	harnessBytes := max(len(systemPrompt)-contextFileBytes-len(skillCatalogue), 0)
	sources.systemSources = []contextsource.Source{
		contextsource.NamedFromBytes(harnessContextSourceName, harnessBytes),
	}
	if skillCatalogue != "" {
		skillCount := len(skills)
		sources.sessionSources = append(sources.sessionSources, contextsource.NamedFromBytes(
			fmt.Sprintf(skillCatalogueSourceNameFormat, skillCount, util.PluralNoun(skillCount, "skill")),
			len(skillCatalogue),
		))
	}
	return sources
}

func currentContextSources(
	staticSources staticContextSources,
	toolSource *contextsource.Source,
	events []agent.Event,
) commands.ContextSources {
	systemSources := slices.Clone(staticSources.systemSources)
	systemSources = append(systemSources, staticSources.systemFiles...)
	if toolSource != nil {
		systemSources = append(systemSources, *toolSource)
	}
	sessionSources := slices.Clone(staticSources.sessionSources)
	sessionSources = append(sessionSources, skill.LoadedSkillSources(events)...)
	return commands.ContextSources{
		SystemSources:  systemSources,
		ProjectSources: slices.Clone(staticSources.projectFiles),
		SessionSources: sessionSources,
	}
}
