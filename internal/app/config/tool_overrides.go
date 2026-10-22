package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/async"
	"crdx.org/oh/pkg/tool/middleware/configure"
)

type BuiltinToolOptions struct {
	IsGroupAllowed func(string) bool
	GroupForTool   func(string) string
	Approve        func(context.Context, string, string, permission.Setting) error
}

func (self Config) ConfigureBuiltins(subjects []tool.Tool, options BuiltinToolOptions) ([]tool.Tool, error) {
	configuredTools := make([]tool.Tool, 0, len(subjects))
	for _, subject := range subjects {
		setting, isConfigured := self.Tools[subject.Name()]
		if options.GroupForTool != nil {
			if group := options.GroupForTool(subject.Name()); group != "" {
				setting.Group = group
				isConfigured = true
			} else {
				setting.Group = ""
			}
		}
		if !isConfigured || !setting.offered() {
			if !isConfigured {
				configuredTools = append(configuredTools, subject)
			}
			continue
		}

		if setting.Concurrency > 1 && !subject.Concurrent() {
			return nil, self.complain(subject.Name(), errors.New("concurrency above one is unavailable for this built-in tool"))
		}
		overlay := configure.Options{
			Description: setting.Description,
			Concurrency: setting.Concurrency,
			TimeLimit:   setting.Timeout,
		}
		if setting.Group != "" {
			if options.IsGroupAllowed == nil {
				return nil, self.complain(subject.Name(), errors.New("group access cannot be checked"))
			}
			group := setting.Group
			overlay.RequireAccess = func() error {
				if !options.IsGroupAllowed(group) {
					return fmt.Errorf("%s access unavailable; ctrl+x %s grants it", subject.Name(), group)
				}
				return nil
			}
		}
		approve, err := self.builtinApproval(subject.Name(), setting.Permission, options.Approve)
		if err != nil {
			return nil, err
		}
		overlay.Approve = approve
		configuredTools = append(configuredTools, configure.Tool(subject, overlay))
	}
	return configuredTools, nil
}

func (self Config) builtinApproval(
	name string,
	permissionValue Permission,
	approve func(context.Context, string, string, permission.Setting) error,
) (func(context.Context, string) error, error) {
	if self.getSourcePath(toolsSetting, name, "permission") == "" {
		return func(context.Context, string) error { return nil }, nil
	}
	setting, err := permissionValue.setting()
	if err != nil {
		return nil, self.complain(name, fmt.Errorf("permission: %w", err))
	}
	if setting.IsAllowed() || name == "lookup" || name == "fetch" {
		return func(context.Context, string) error { return nil }, nil
	}
	if approve == nil {
		return nil, self.complain(name, errors.New("permission requires an approval handler"))
	}
	return func(ctx context.Context, arguments string) error {
		if name == "bash" && isHostNetworkCall(arguments) {
			return nil
		}
		return approve(ctx, name, arguments, setting)
	}, nil
}

func isHostNetworkCall(arguments string) bool {
	var call struct {
		Network string `json:"network"`
	}
	return json.Unmarshal([]byte(arguments), &call) == nil && call.Network == "host"
}

func (self Config) validateToolSettings() error {
	for _, source := range self.sources {
		for _, key := range source.meta.Undecoded() {
			if len(key) > 0 && (key[0] == toolsSetting || key[0] == "tools") {
				return fmt.Errorf("%s: unknown tool setting %s", source.path, key.String())
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(self.Tools)) {
		setting := self.Tools[name]
		if path := self.getSourcePath(toolsSetting, name, hostNetworkSetting); path != "" && name != "bash" {
			return fmt.Errorf("%s: %s.%s.%s is only valid for bash", path, toolsSetting, name, hostNetworkSetting)
		}
		if setting.IsBackgroundable != nil && *setting.IsBackgroundable {
			if toolset.IsBuiltin(name) && !slices.Contains([]string{"bash", "grep", "lookup"}, name) {
				return self.complain(name, errors.New("this built-in tool cannot run in the background"))
			}
			for _, parameter := range setting.Parameters {
				if async.IsReserved(parameter.Name) {
					return self.complain(name, fmt.Errorf("parameter %s is reserved for task execution", parameter.Name))
				}
			}
		}
		if !toolset.IsBuiltin(name) {
			continue
		}
		if strings.TrimSpace(setting.Description) == "" && self.getSourcePath(toolsSetting, name, "description") != "" {
			return self.complain(name, errors.New("description is empty"))
		}
		if setting.Timeout < 0 {
			return self.complain(name, errors.New("timeout must not be negative"))
		}
		if setting.Timeout > 0 && !slices.Contains([]string{"bash", "grep", "lookup", "fetch", "wait", "notify"}, name) {
			return self.complain(name, errors.New("timeout is unavailable for this built-in tool"))
		}
		if setting.Concurrency < 0 {
			return self.complain(name, errors.New("concurrency must not be negative"))
		}
		if _, err := setting.Permission.setting(); err != nil {
			return self.complain(name, fmt.Errorf("permission: %w", err))
		}
		if _, err := setting.HostNetwork.setting(); err != nil {
			return self.complain(name, fmt.Errorf("%s: %w", hostNetworkSetting, err))
		}
		for _, field := range []string{"command", "parameters", "subject", "version"} {
			if self.getSourcePath(toolsSetting, name, field) != "" {
				return self.complain(name, fmt.Errorf("%s cannot replace a built-in tool", field))
			}
		}
	}
	return nil
}
