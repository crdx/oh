package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/internal/util/pathutil"
)

const (
	settingFlag      = "--set"
	appendedOperator = "+="
	assignOperator   = "="
)

func CommandLineSetting(directory string, setting string) Source {
	return Source{Path: directory, Setting: setting}
}

func (self Source) isSetting() bool {
	return self.Setting != ""
}

func (self Source) isVersioned() bool {
	return !self.IsOverride && !self.isSetting()
}

func (self Source) displayPath() string {
	if self.isSetting() {
		return settingFlag + " " + self.Setting
	}
	return pathutil.Shorten(self.Path)
}

type writtenSetting struct {
	key        string
	path       toml.Key
	value      string
	isAppended bool
}

func parseSetting(text string) (writtenSetting, error) {
	key, value, hasValue := strings.Cut(text, assignOperator)
	if !hasValue {
		return writtenSetting{}, fmt.Errorf(tr.SettingShape, settingFlag)
	}

	setting := writtenSetting{key: key, value: strings.TrimSpace(value)}
	if prefix, isAppended := strings.CutSuffix(setting.key, "+"); isAppended {
		setting.key = prefix
		setting.isAppended = true
	}
	setting.key = strings.TrimSpace(setting.key)

	path, err := settingPath(setting.key)
	if err != nil {
		return writtenSetting{}, err
	}
	setting.path = path
	return setting, nil
}

func settingPath(key string) (toml.Key, error) {
	var document map[string]any
	meta, err := toml.Decode(key+" = true", &document)
	if err != nil || key == "" {
		return nil, fmt.Errorf(tr.SettingNameInvalid, key)
	}

	var path toml.Key
	for _, candidate := range meta.Keys() {
		if meta.Type(candidate...) != "Hash" {
			if path != nil {
				return nil, fmt.Errorf(tr.SettingNameInvalid, key)
			}
			path = candidate
		}
	}
	if path == nil {
		return nil, fmt.Errorf(tr.SettingNameInvalid, key)
	}
	return path, nil
}

func (self writtenSetting) document(config *Config) (string, error) {
	value, isTOML := self.decodedValue()
	if !self.isAppended {
		if isTOML {
			return self.key + " = " + self.value, nil
		}
		return self.key + " = " + quotedText(self.value), nil
	}

	additions, err := self.appendedEntries(value, isTOML)
	if err != nil {
		return "", err
	}
	current, err := currentEntries(reflect.ValueOf(*config), self.path)
	if err != nil {
		return "", err
	}

	entries := make([]string, 0, len(current)+len(additions))
	for _, entry := range slices.Concat(current, additions) {
		entries = append(entries, quotedText(entry))
	}
	return self.key + " = [" + strings.Join(entries, ", ") + "]", nil
}

func (self writtenSetting) decodedValue() (any, bool) {
	var document map[string]any
	meta, err := toml.Decode(self.key+" = "+self.value, &document)
	if err != nil {
		return nil, false
	}
	for _, key := range meta.Keys() {
		if !isKeyPrefix(key, self.path) && !isKeyPrefix(self.path, key) {
			return nil, false
		}
	}

	var value any = document
	for _, name := range self.path {
		table, isTable := value.(map[string]any)
		if !isTable {
			return nil, false
		}
		value = table[name]
	}
	return value, true
}

func (self writtenSetting) appendedEntries(value any, isTOML bool) ([]string, error) {
	if !isTOML {
		return []string{self.value}, nil
	}

	switch entryValue := value.(type) {
	case string:
		return []string{entryValue}, nil
	case []any:
		entries := make([]string, 0, len(entryValue))
		for _, entry := range entryValue {
			text, isText := entry.(string)
			if !isText {
				return nil, fmt.Errorf(tr.AppendedNotText, self.path.String())
			}
			entries = append(entries, text)
		}
		return entries, nil
	default:
		return nil, fmt.Errorf(tr.AppendedNotText, self.path.String())
	}
}

func currentEntries(value reflect.Value, path toml.Key) ([]string, error) {
	for _, name := range path {
		next, isFound := settingField(value, name)
		if !isFound {
			return nil, nil
		}
		value = next
	}

	if value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice {
		return nil, fmt.Errorf(tr.AppendedNotList, path.String())
	}

	entries := make([]string, 0, value.Len())
	for index := range value.Len() {
		entry := value.Index(index)
		if entry.Kind() == reflect.Interface {
			entry = entry.Elem()
		}
		if entry.Kind() != reflect.String {
			return nil, fmt.Errorf(tr.AppendedNotList, path.String())
		}
		entries = append(entries, entry.String())
	}
	return entries, nil
}

func settingField(value reflect.Value, name string) (reflect.Value, bool) {
	if value.Kind() == reflect.Interface {
		value = value.Elem()
	}

	if value.Kind() == reflect.Map && value.Type().Key().Kind() == reflect.String {
		entry := value.MapIndex(reflect.ValueOf(name).Convert(value.Type().Key()))
		return entry, entry.IsValid()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	for index := range value.NumField() {
		field := value.Type().Field(index)
		if !field.IsExported() {
			continue
		}
		tag, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		if tag == name || (tag == "" && strings.EqualFold(field.Name, name)) {
			return value.Field(index), true
		}
	}
	return reflect.Value{}, false
}

func isKeyPrefix(prefix toml.Key, key toml.Key) bool {
	return len(prefix) <= len(key) && slices.Equal(prefix, key[:len(prefix)])
}

func quotedText(text string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range text {
		switch {
		case character == '"' || character == '\\':
			builder.WriteByte('\\')
			builder.WriteRune(character)
		case character < ' ' || character == 0x7f:
			_, _ = fmt.Fprintf(&builder, `\u%04X`, character)
		default:
			builder.WriteRune(character)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func applySetting(config *Config, source Source) error {
	displayPath := source.displayPath()

	setting, err := parseSetting(source.Setting)
	if err != nil {
		return fmt.Errorf("%s: %w", displayPath, err)
	}
	document, err := setting.document(config)
	if err != nil {
		return fmt.Errorf("%s: %w", displayPath, err)
	}

	return applyDocument(config, source, displayPath, []byte(document))
}

func (self Config) CommandLineSettings() []string {
	var settings []string
	for _, source := range self.sources {
		if !source.source.isSetting() {
			continue
		}
		for _, name := range definedSettings(source.meta) {
			if !slices.Contains(settings, name) {
				settings = append(settings, name)
			}
		}
	}
	return settings
}

func definedSettings(meta *toml.MetaData) []string {
	var settings []string
	for _, key := range meta.Keys() {
		if !meta.IsDefined(key...) || meta.Type(key...) == "Hash" || key.String() == versionSetting {
			continue
		}
		if name := leafTableOf(key).String(); !slices.Contains(settings, name) {
			settings = append(settings, name)
		}
	}
	return settings
}
