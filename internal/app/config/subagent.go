package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/tr"
)

const (
	subagentModelName    = "model"
	subagentModelSetting = subagentSetting + "." + subagentModelName
)

var subagentLabelPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type Availability string

const (
	AvailableToAll          Availability = "all"
	AvailableToSameProvider Availability = "same-provider"
)

type SubagentModel struct {
	ModelChoice

	Label        string       `toml:"label"`
	Description  string       `toml:"description"`
	SystemPrompt string       `toml:"system_prompt"`
	Availability Availability `toml:"availability"`
}

func applySubagentModels(settings *Subagent, meta toml.MetaData, data []byte, directory string) error {
	if err := replaceSubagentModels(settings, meta, data); err != nil {
		return err
	}
	return normaliseSubagentModels(settings, directory)
}

func replaceSubagentModels(settings *Subagent, meta toml.MetaData, data []byte) error {
	if !meta.IsDefined(subagentSetting, subagentModelName) {
		return nil
	}
	var document struct {
		Subagent struct {
			Models []SubagentModel `toml:"model"`
		} `toml:"subagent"`
	}
	if _, err := toml.Decode(string(data), &document); err != nil {
		return err
	}
	settings.Models = document.Subagent.Models
	return nil
}

func normaliseSubagentModels(settings *Subagent, directory string) error {
	var labels []string
	for i := range settings.Models {
		entry := &settings.Models[i]
		name := fmt.Sprintf("%s[%d]", subagentModelSetting, i+1)
		if err := normaliseSubagentModelChoice(entry, name, directory); err != nil {
			return err
		}
		entry.Label = strings.TrimSpace(entry.Label)
		switch {
		case entry.Label == "" && len(entry.RoundRobin) > 0:
			return fmt.Errorf(tr.RotationNeedsLabel, name)
		case entry.Label == "":
		case !subagentLabelPattern.MatchString(entry.Label):
			return fmt.Errorf(tr.LabelShape, name+".label", entry.Label)
		case slices.Contains(labels, entry.Label):
			return fmt.Errorf(tr.LabelRepeated, name+".label", entry.Label)
		default:
			labels = append(labels, entry.Label)
		}
		entry.Description = strings.TrimSpace(entry.Description)
		entry.SystemPrompt = strings.TrimSpace(entry.SystemPrompt)
		switch entry.Availability {
		case "":
			entry.Availability = AvailableToSameProvider
		case AvailableToAll, AvailableToSameProvider:
		default:
			return fmt.Errorf(tr.AvailabilityChoice, name+".availability", AvailableToAll, AvailableToSameProvider)
		}
	}
	return nil
}

func normaliseSubagentModelChoice(entry *SubagentModel, name string, directory string) error {
	entry.Model = strings.TrimSpace(entry.Model)
	switch {
	case entry.Model != "" && entry.RoundRobin != nil:
		return fmt.Errorf(tr.SettingsInConflict, name+"."+modelSetting, name+"."+roundRobinSetting)
	case entry.Model != "":
		return nil
	case entry.RoundRobin != nil:
		return applyRoundRobin(&entry.ModelChoice, directory, name+"."+roundRobinSetting)
	default:
		return fmt.Errorf(tr.SubagentModelMissing, name, modelSetting, roundRobinSetting)
	}
}
