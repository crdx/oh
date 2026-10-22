package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
)

const (
	toolsSetting       = "tool"
	hostNetworkSetting = "host_network"
)

type ToolConfig struct {
	IsOffered        *bool             `toml:"enabled"`
	IsGranted        bool              `toml:"granted"`
	IsBackgroundable *bool             `toml:"background"`
	HostNetwork      Permission        `toml:"host_network"`
	Description      string            `toml:"description"`
	Command          []string          `toml:"command"`
	Parameters       []CustomParameter `toml:"parameters"`
	Subject          string            `toml:"subject"`
	Timeout          time.Duration     `toml:"timeout"`
	Concurrency      int               `toml:"concurrency"`
	Permission       Permission        `toml:"permission"`
	Group            string            `toml:"group"`
	Version          int               `toml:"version"`
}

type Permission struct {
	Rule    string
	Timeout time.Duration
}

type CustomParameter struct {
	Name        string   `toml:"name"`
	Kind        string   `toml:"kind"`
	Description string   `toml:"description"`
	Values      []string `toml:"values"`
	IsOptional  bool     `toml:"optional"`
}

func (self *Permission) UnmarshalTOML(value any) error {
	switch configuredValue := value.(type) {
	case string:
		if strings.TrimSpace(configuredValue) == "" {
			return errors.New("permission is empty")
		}
		*self = Permission{Rule: configuredValue}
		return nil
	case map[string]any:
		return self.unmarshalTable(configuredValue)
	default:
		return errors.New("permission is not a rule or a table")
	}
}

func (self *Permission) unmarshalTable(configuredTable map[string]any) error {
	var unknown []string
	for name := range configuredTable {
		if name != "rule" && name != "timeout" {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("unknown: %s", strings.Join(unknown, ", "))
	}

	ruleValue, hasRule := configuredTable["rule"]
	if !hasRule {
		return errors.New("rule is missing")
	}
	rule, isText := ruleValue.(string)
	if !isText {
		return errors.New("rule is not text")
	}
	if strings.TrimSpace(rule) == "" {
		return errors.New("rule is empty")
	}

	var timeout time.Duration
	if timeoutValue, hasTimeout := configuredTable["timeout"]; hasTimeout {
		configuredTimeout, isText := timeoutValue.(string)
		if !isText {
			return errors.New("timeout is not a duration")
		}
		var err error
		if timeout, err = time.ParseDuration(configuredTimeout); err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
		if timeout <= 0 {
			return fmt.Errorf("timeout must be positive, got %q", configuredTimeout)
		}
	}

	*self = Permission{Rule: rule, Timeout: timeout}
	return nil
}

func (self *Permission) setting() (permission.Setting, error) {
	rule := permission.Ask
	if strings.TrimSpace(self.Rule) != "" {
		var err error
		if rule, err = permission.ParseRule(self.Rule); err != nil {
			return permission.Setting{}, err
		}
	}
	if rule == permission.Allow && self.Timeout != 0 {
		return permission.Setting{}, fmt.Errorf("timeout applies only when rule is %q", permission.Ask)
	}

	return permission.Setting{Rule: rule, Timeout: self.Timeout}, nil
}

func (self ToolConfig) offered() bool { return self.IsOffered == nil || *self.IsOffered }

func (self Config) BuildCustomTools(options command.Options) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(self.Tools))

	for _, name := range self.CustomToolNames() {
		customTool, err := self.buildCustomTool(name, options)
		if err != nil {
			return nil, self.complain(name, err)
		}

		tools = append(tools, customTool)
	}

	return tools, nil
}

func (self Config) CustomToolNames() []string {
	names := slices.Sorted(maps.Keys(self.Tools))
	return slices.DeleteFunc(names, func(name string) bool {
		return toolset.IsBuiltin(name) || !self.Tools[name].offered()
	})
}

func (self Config) IsToolBackgroundable(name string) bool {
	setting := self.Tools[name]
	if setting.IsBackgroundable != nil {
		return *setting.IsBackgroundable
	}
	return toolset.IsBuiltin(name) && slices.Contains([]string{"bash", "grep", "lookup"}, name)
}

func (self Config) IsToolOffered(name string) bool {
	setting, isConfigured := self.Tools[name]
	return !isConfigured || setting.offered()
}

func (self Config) ToolGroupNames() []string {
	names := slices.Sorted(maps.Keys(self.Tools))
	return slices.DeleteFunc(names, func(name string) bool { return !self.Tools[name].offered() })
}

func (self Config) ToolGroups() (caps.ToolGroups, error) {
	groups := make(caps.ToolGroups)
	for _, name := range slices.Sorted(maps.Keys(self.Tools)) {
		setting := self.Tools[name]
		group := setting.Group
		if group == "" {
			if !setting.IsGranted && self.getSourcePath(toolsSetting, name, "granted") != "" {
				return nil, self.complain(name, errors.New("granted = false has no effect without a group; add a group or remove the setting"))
			}
			continue
		}
		if utf8.RuneCountInString(group) != 1 || group[0] < 'a' || group[0] > 'z' {
			return nil, self.complain(name, fmt.Errorf("group %q is not one lowercase letter", group))
		}
		groups[group] = append(groups[group], name)
	}
	return groups, nil
}

func (self Config) DefaultToolGroupFlags(selectedToolNames []string) string {
	flags := make(map[string]struct{})
	for _, name := range self.ToolGroupNames() {
		setting := self.Tools[name]
		if !setting.IsGranted || setting.Group == "" {
			continue
		}
		if len(selectedToolNames) > 0 && !slices.Contains(selectedToolNames, name) {
			continue
		}
		flags[setting.Group] = struct{}{}
	}
	return strings.Join(slices.Sorted(maps.Keys(flags)), "")
}

func (self Config) complain(name string, err error) error {
	setting := toolsSetting + "." + name
	if path := self.getSourcePath(toolsSetting, name); path != "" {
		setting = path + ": " + setting
	}

	return fmt.Errorf("%s: %w", setting, err)
}

func (self Config) buildCustomTool(name string, options command.Options) (tool.Tool, error) {
	declaration, err := self.declare(name)
	if err != nil {
		return nil, err
	}
	if options.GroupForTool != nil {
		declaration.Group = options.GroupForTool(name)
	}
	if self.IsToolBackgroundable(name) && declaration.TimeLimit == 0 {
		declaration.ManagedLimit = tasks.RunLimit
	}

	return command.New(declaration, options)
}

func (self Config) declare(name string) (command.Declaration, error) {
	setting := self.Tools[name]
	if setting.Group != "" && (utf8.RuneCountInString(setting.Group) != 1 || setting.Group[0] < 'a' || setting.Group[0] > 'z') {
		return command.Declaration{}, fmt.Errorf("group %q is not one lowercase letter", setting.Group)
	}

	approval, err := setting.Permission.setting()
	if err != nil {
		return command.Declaration{}, fmt.Errorf("permission: %w", err)
	}

	resolvedCommand, err := self.resolveCommand(name, setting.Command)
	if err != nil {
		return command.Declaration{}, err
	}

	parameters := make([]command.Parameter, 0, len(setting.Parameters))

	for _, parameter := range setting.Parameters {
		parameters = append(parameters, command.Parameter{
			Name:        parameter.Name,
			Kind:        command.Kind(parameter.Kind),
			Description: parameter.Description,
			Values:      parameter.Values,
			IsOptional:  parameter.IsOptional,
		})
	}

	return command.Declaration{
		Name:            name,
		Description:     setting.Description,
		Command:         resolvedCommand,
		Parameters:      parameters,
		Subject:         setting.Subject,
		TimeLimit:       setting.Timeout,
		Concurrency:     setting.Concurrency,
		MustAsk:         !approval.IsAllowed(),
		ApprovalTimeout: approval.Timeout,
		Group:           setting.Group,
		Version:         setting.Version,
	}, nil
}

func (self Config) resolveCommand(name string, writtenCommand []string) ([]string, error) {
	sourcePath := self.getSourceFile(toolsSetting, name)
	resolvedCommand := slices.Clone(writtenCommand)

	for at, word := range resolvedCommand {
		if !isCommandPath(at, word) {
			continue
		}

		path, err := resolveConfigPath(sourcePath, word)
		if err != nil {
			return nil, fmt.Errorf("command: %w", err)
		}
		resolvedCommand[at] = path
	}

	return resolvedCommand, nil
}

func isCommandPath(at int, word string) bool {
	if at == 0 {
		return strings.ContainsRune(word, '/')
	}

	return strings.HasPrefix(word, "./") || strings.HasPrefix(word, "../")
}
