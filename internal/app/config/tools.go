package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
)

const toolsSetting = "tools"

type CustomTool struct {
	Description string            `toml:"description"`
	Command     []string          `toml:"command"`
	Parameters  []CustomParameter `toml:"parameters"`
	Subject     string            `toml:"subject"`
	Timeout     time.Duration     `toml:"timeout"`
	Permission  string            `toml:"permission"`
}

type CustomParameter struct {
	Name        string   `toml:"name"`
	Kind        string   `toml:"kind"`
	Description string   `toml:"description"`
	Values      []string `toml:"values"`
	IsOptional  bool     `toml:"optional"`
}

func (self CustomTool) IsReference() bool {
	return len(self.Command) == 0
}

func (self CustomTool) CheckReference() error {
	if !self.IsReference() {
		return nil
	}

	for setting, isWritten := range map[string]bool{
		"description": strings.TrimSpace(self.Description) != "",
		"parameters":  len(self.Parameters) > 0,
		"subject":     strings.TrimSpace(self.Subject) != "",
		"timeout":     self.Timeout != 0,
		"permission":  strings.TrimSpace(self.Permission) != "",
	} {
		if isWritten {
			return fmt.Errorf("%s: not allowed when naming a built-in tool", setting)
		}
	}

	return nil
}

func (self Config) BuildCustomTools(options command.Options) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(self.Tools))

	for _, name := range slices.Sorted(maps.Keys(self.Tools)) {
		customTool, err := self.buildCustomTool(name, options)
		if err != nil {
			return nil, self.complain(name, err)
		}

		tools = append(tools, customTool)
	}

	return tools, nil
}

func (self Config) CustomToolNames() []string {
	return slices.Sorted(maps.Keys(self.Tools))
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

	return command.New(declaration, options)
}

func (self Config) declare(name string) (command.Declaration, error) {
	return Declare(name, self.Tools[name], self.getSourceFile(toolsSetting, name))
}

func Declare(name string, declaration CustomTool, sourcePath string) (command.Declaration, error) {
	rule := permission.Ask
	if strings.TrimSpace(declaration.Permission) != "" {
		var err error
		if rule, err = permission.ParseRule(declaration.Permission); err != nil {
			return command.Declaration{}, fmt.Errorf("permission: %w", err)
		}
	}

	resolvedCommand, err := ResolveCommand(sourcePath, declaration.Command)
	if err != nil {
		return command.Declaration{}, err
	}

	parameters := make([]command.Parameter, 0, len(declaration.Parameters))

	for _, parameter := range declaration.Parameters {
		parameters = append(parameters, command.Parameter{
			Name:        parameter.Name,
			Kind:        command.Kind(parameter.Kind),
			Description: parameter.Description,
			Values:      parameter.Values,
			IsOptional:  parameter.IsOptional,
		})
	}

	return command.Declaration{
		Name:        name,
		Description: declaration.Description,
		Command:     resolvedCommand,
		Parameters:  parameters,
		Subject:     declaration.Subject,
		TimeLimit:   declaration.Timeout,
		MustAsk:     rule != permission.Allow,
	}, nil
}

func ResolveCommand(sourcePath string, writtenCommand []string) ([]string, error) {
	resolvedCommand := slices.Clone(writtenCommand)

	for at, word := range resolvedCommand {
		if !isCommandPath(at, word) {
			continue
		}

		path, err := ResolveWrittenPath(sourcePath, word)
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
