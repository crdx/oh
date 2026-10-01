package style

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	toolDefaultKey = "default"
	toolNameKey    = "name"
	toolPaintKey   = "paint"
	toolFocusKey   = "focus"
)

type ToolEntry struct {
	Default ToolAppearance
	Actions map[string]ToolAppearance
}

type ToolTheme map[string]ToolEntry

func (self *ToolTheme) Clone() ToolTheme {
	if *self == nil {
		return nil
	}

	duplicate := make(ToolTheme, len(*self))
	for tool, entry := range *self {
		entry.Actions = maps.Clone(entry.Actions)
		duplicate[tool] = entry
	}

	return duplicate
}

func (self *ToolTheme) UnmarshalTOML(data any) error {
	tools, isTable := data.(map[string]any)
	if !isTable {
		return fmt.Errorf("ui.theme.tool must be a table of tools, not %T", data)
	}

	theme := self.Clone()
	if theme == nil {
		theme = make(ToolTheme, len(tools))
	}

	for _, tool := range slices.Sorted(maps.Keys(tools)) {
		if err := theme.mergeTool(tool, tools[tool]); err != nil {
			return err
		}
	}

	*self = theme

	return nil
}

func mergeAppearance(current ToolAppearance, path string, data map[string]any) (ToolAppearance, error) {
	for _, key := range slices.Sorted(maps.Keys(data)) {
		text, isText := data[key].(string)
		if !isText {
			return current, fmt.Errorf("%s.%s must be a string, not %T", path, key, data[key])
		}

		var err error
		switch key {
		case toolNameKey:
			err = current.Name.UnmarshalText([]byte(text))
		case toolPaintKey:
			err = current.Paint.UnmarshalText([]byte(text))
		case toolFocusKey:
			err = current.Focus.UnmarshalText([]byte(text))
		default:
			return current, fmt.Errorf(
				"%s has the unknown key %q: an appearance takes %s, %s and %s",
				path, key, toolNameKey, toolPaintKey, toolFocusKey,
			)
		}
		if err != nil {
			return current, fmt.Errorf("%s.%s: %w", path, key, err)
		}
	}

	return current, nil
}

func (self *ToolTheme) Resolved() map[string]ToolAppearance {
	kinds := make(map[string]ToolAppearance, len(*self))
	for tool, entry := range *self {
		kinds[tool] = entry.Default
		for action, appearance := range entry.Actions {
			kinds[toolKind(tool, action)] = entry.resolve(action, appearance)
		}
	}

	return kinds
}

func (self *ToolTheme) mergeTool(tool string, data any) error {
	path := "ui.theme.tool." + tool
	entries, isTable := data.(map[string]any)
	if !isTable {
		return fmt.Errorf("%s must be a table holding the tool's default appearance and its actions, not %T", path, data)
	}

	entry := (*self)[tool]
	entry.Actions = maps.Clone(entry.Actions)

	for _, key := range slices.Sorted(maps.Keys(entries)) {
		appearanceData, isAppearanceTable := entries[key].(map[string]any)
		if !isAppearanceTable {
			return fmt.Errorf(
				"%s.%s must be a table: put the tool's own name and paint under %s.%s, and each action beside it",
				path, key, path, toolDefaultKey,
			)
		}

		if key == toolDefaultKey {
			appearance, err := mergeAppearance(entry.Default, path+"."+key, appearanceData)
			if err != nil {
				return err
			}
			entry.Default = appearance
			continue
		}

		appearance, err := mergeAppearance(entry.Actions[key], path+"."+key, appearanceData)
		if err != nil {
			return err
		}
		if entry.Actions == nil {
			entry.Actions = make(map[string]ToolAppearance)
		}
		entry.Actions[key] = appearance
	}

	(*self)[tool] = entry

	return nil
}

func (self ToolEntry) resolve(action string, appearance ToolAppearance) ToolAppearance {
	if appearance.Name == "" {
		appearance.Name = ToolName(action)
	}
	if appearance.Paint == "" {
		appearance.Paint = self.Default.Paint
	}
	if appearance.Focus == "" {
		appearance.Focus = self.Default.Focus
	}

	return appearance
}

func toolKind(tool string, action string) string {
	return strings.Join([]string{tool, action}, "_")
}
