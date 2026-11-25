package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/tr"
)

func TestSubagentsOfferNothingByDefault(t *testing.T) {
	settings := configFrom(t, "")
	if settings.Subagent.IsFree || len(settings.Subagent.Models) != 0 {
		t.Errorf("the default subagent settings are %+v", settings.Subagent)
	}
}

func TestSubagentModelsTakeAModelOrALabelledRotation(t *testing.T) {
	settings := configFrom(t, `
		[subagent]
		free = false

		[[subagent.model]]
		label = " explorer "
		round_robin = ["anthropic/haiku@high", "codex/luna@medium"]
		description = " reading code "
		system_prompt = " You explore. "
		availability = "all"

		[[subagent.model]]
		model = " anthropic/fable@high "
	`)
	got := settings.Subagent.Models
	if len(got) != 2 {
		t.Fatalf("got %d subagent models, want 2", len(got))
	}
	if got[0].Label != "explorer" || got[0].Description != "reading code" || got[0].SystemPrompt != "You explore." ||
		got[0].Availability != AvailableToAll || !slices.Equal(got[0].Rotation(), []string{"anthropic/haiku@high", "codex/luna@medium"}) {
		t.Errorf("the labelled rotation decoded as %+v", got[0])
	}
	if got[1].Label != "" || got[1].Availability != AvailableToSameProvider || !slices.Equal(got[1].Rotation(), []string{"anthropic/fable@high"}) {
		t.Errorf("the single model decoded as %+v", got[1])
	}
	if unknown := settings.UnknownSettings(); len(unknown) != 0 {
		t.Errorf("subagent models were reported as unknown: %q", unknown)
	}
}

func TestASubagentRotationCanBeReadFromAFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "explorers.txt"), []byte("# explorers\nanthropic/haiku@high\n\ncodex/luna@medium\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "[[subagent.model]]\nlabel = \"explorer\"\nround_robin = \"explorers.txt\"\n"); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.Subagent.Models[0].Rotation(); !slices.Equal(got, []string{"anthropic/haiku@high", "codex/luna@medium"}) {
		t.Errorf("the rotation file gave %q", got)
	}
	snapshots, watched := settings.rotationFiles()
	resolved := filepath.Join(directory, "explorers.txt")
	if _, isWatched := snapshots[resolved]; !isWatched || !slices.Equal(watched[resolved], []string{"subagent.model"}) {
		t.Errorf("the rotation file is not watched: %v", watched)
	}
}

func TestAnUnknownSubagentModelSettingIsReported(t *testing.T) {
	settings := configFrom(t, `
		[[subagent.model]]
		model = "anthropic/haiku"
		colour = "blue"
	`)
	if unknown := strings.Join(settings.UnknownSettings(), "\n"); !strings.Contains(unknown, "colour") {
		t.Errorf("a misspelt subagent model setting was not reported: %q", unknown)
	}
}

func TestSubagentModelsRefuseWhatCannotBeOffered(t *testing.T) {
	for body, want := range map[string]string{
		"label = \"explorer\"": fmt.Sprintf(tr.SubagentModelMissing, "subagent.model[2]", "model", "round_robin"),
		"model = \" \"":        fmt.Sprintf(tr.SubagentModelMissing, "subagent.model[2]", "model", "round_robin"),
		"model = \"a/b\"\nround_robin = [\"c/d\"]\nlabel = \"x\"": fmt.Sprintf(tr.SettingsInConflict, "subagent.model[2].model", "subagent.model[2].round_robin"),
		"round_robin = [\"a/b\", \"c/d\"]":                        fmt.Sprintf(tr.RotationNeedsLabel, "subagent.model[2]"),
		"round_robin = []\nlabel = \"x\"":                         fmt.Sprintf(tr.RoundRobinEmpty, "subagent.model[2].round_robin"),
		"round_robin = [\"a/b\", \" \"]\nlabel = \"x\"":           fmt.Sprintf(tr.EmptySelection, "subagent.model[2].round_robin"),
		"model = \"a/b\"\nlabel = \"Explorer\"":                   fmt.Sprintf(tr.LabelShape, "subagent.model[2].label", "Explorer"),
		"model = \"a/b\"\nlabel = \"code/explorer\"":              fmt.Sprintf(tr.LabelShape, "subagent.model[2].label", "code/explorer"),
		"model = \"a/b\"\nlabel = \"haiku\"":                      fmt.Sprintf(tr.LabelRepeated, "subagent.model[2].label", "haiku"),
		"model = \"a/b\"\navailability = \"some\"":                fmt.Sprintf(tr.AvailabilityChoice, "subagent.model[2].availability", AvailableToAll, AvailableToSameProvider),
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := writeConfigFile(path, "[[subagent.model]]\nmodel = \"anthropic/haiku\"\nlabel = \"haiku\"\n\n[[subagent.model]]\n"+body+"\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want an error containing %q", body, err, want)
		}
	}
}

func TestAnOverrideListingSubagentModelsReplacesTheWholeList(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	global := "[[subagent.model]]\nmodel = \"anthropic/haiku@high\"\ndescription = \"exploration work\"\navailability = \"all\"\n\n" +
		"[[subagent.model]]\nmodel = \"anthropic/fable\"\n"
	if err := writeConfigFile(globalPath, global); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[[subagent.model]]\nmodel = \"codex/luna\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSources(Source{Path: globalPath}, Source{Path: overridePath, IsOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.Subagent.Models; len(got) != 1 || got[0].Model != "codex/luna" || got[0].Description != "" || got[0].Availability != AvailableToSameProvider {
		t.Errorf("the override left %+v, want codex/luna alone", got)
	}

	if err := os.WriteFile(overridePath, []byte("[subagent]\nconcurrency = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadSources(Source{Path: globalPath}, Source{Path: overridePath, IsOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Subagent.Models) != 2 || settings.Subagent.Models[0].Description != "exploration work" {
		t.Errorf("an override listing no models changed them to %+v", settings.Subagent.Models)
	}
}
