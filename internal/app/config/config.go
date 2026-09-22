package config

import (
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
	"crdx.org/oh/internal/app/style"

	"crdx.org/oh/internal/format"
	"crdx.org/oh/internal/util/pathutil"
)

//go:embed defaults.toml
var defaultsTOML string

const minimumToolOutputBytes = 1024

const (
	sessionPlaceholder = "{session}"
	hostnameLimit      = 253
)

const (
	versionSetting  = "version"
	snippetsSetting = "snippets"
	uiSetting       = "ui"
	themeSetting    = "theme"
)

type Config struct {
	Version  int                            `toml:"version"`
	Caps     Caps                           `toml:"caps"`
	Editor   Editor                         `toml:"editor"`
	Input    Input                          `toml:"input"`
	Model    Model                          `toml:"model"`
	Provider Provider                       `toml:"provider"`
	Ports    Ports                          `toml:"ports"`
	Snippets map[string]snippets.Definition `toml:"snippets"`
	Tools    map[string]CustomTool          `toml:"tools"`
	Skills   SkillPaths                     `toml:"skills"`
	Sandbox  sandbox                        `toml:"sandbox"`
	Bar      Bar                            `toml:"bar"`
	Ui       Ui                             `toml:"ui"`
	Tool     Tool                           `toml:"tool"`

	Permissions Permissions `toml:"permissions"`

	Experimental map[string]any `toml:"experimental"`

	fallback             *toml.MetaData
	sources              []sourceMetadata
	snippetFileSnapshots map[string]snapshot
}

type sourceMetadata struct {
	source Source
	path   string
	meta   *toml.MetaData
}

type Override struct {
	Path     string
	Settings []string
}

type Caps struct {
	Default DefaultCaps `toml:"default"`
}

type DefaultCaps caps.Set

func (self *DefaultCaps) UnmarshalText(text []byte) error {
	grantedCaps, err := caps.Parse(string(text))
	if err != nil {
		return err
	}
	*self = DefaultCaps(grantedCaps)
	return nil
}

type Editor struct {
	Command editor.Command `toml:"command"`
}

type Input struct {
	Continue string `toml:"continue"`
}

type Model struct {
	RoundRobin []string     `toml:"round_robin"`
	Effort     model.Effort `toml:"effort"`
	IsFast     bool         `toml:"fast"`
}

func (self Model) GetDefaults() model.Defaults {
	return model.Defaults{Effort: self.Effort, IsFast: self.IsFast}
}

type Provider struct {
	Ollama Ollama `toml:"ollama"`
}

type Ollama struct {
	Host string `toml:"host"`
}

type Ports struct {
	Hostname string `toml:"hostname"`
}

func (self Ports) GetHostname(sessionName string, fallbackHostname string) string {
	if self.Hostname == "" {
		return fallbackHostname
	}
	return strings.ReplaceAll(self.Hostname, sessionPlaceholder, sessionName)
}

type Ui struct {
	StreamingMode      output.StreamingMode      `toml:"streaming"`
	Grouping           output.Grouping           `toml:"grouping"`
	ReasoningRendering output.ReasoningRendering `toml:"reasoning"`
	Currency           string                    `toml:"currency"`
	Theme              style.Theme               `toml:"theme"`
}

type Tool struct {
	Output Size `toml:"output"`
}

type Permissions struct {
	Network string `toml:"network"`
	Lookup  string `toml:"lookup"`
	Fetch   string `toml:"fetch"`
}

func (self Config) BuildPermissions() (permission.Set, error) {
	return self.Permissions.build()
}

func (self Permissions) build() (permission.Set, error) {
	var set permission.Set

	for _, entry := range []struct {
		key   string
		value string
		rule  *permission.Rule
	}{
		{key: "network", value: self.Network, rule: &set.Network},
		{key: "lookup", value: self.Lookup, rule: &set.Lookup},
		{key: "fetch", value: self.Fetch, rule: &set.Fetch},
	} {
		rule, err := permission.ParseRule(entry.value)
		if err != nil {
			return permission.Set{}, fmt.Errorf("permissions.%s: %w", entry.key, err)
		}
		*entry.rule = rule
	}

	return set, nil
}

type SkillPaths struct {
	Include []string `toml:"include"`
	Exclude []string `toml:"exclude"`
}

type sandbox = shell.Paths

type Bar struct {
	Top    Rule `toml:"top"`
	Bottom Rule `toml:"bottom"`
}

type Rule struct {
	Left   []toml.Primitive `toml:"left"`
	Center []toml.Primitive `toml:"center"`
	Right  []toml.Primitive `toml:"right"`
}

type LiveConfig struct {
	ContinueMessage    string
	EditorCommand      editor.Command
	SegmentLayout      segment.Layout
	SnippetCommandSet  slash.CommandSet
	StreamingMode      output.StreamingMode
	Grouping           output.Grouping
	ReasoningRendering output.ReasoningRendering
	Theme              style.Theme
	ToolOutputBytes    int
	Permissions        permission.Set
	Experimental       map[string]any
	UnknownSettings    []string
}

func (self Config) BuildLive(registry segment.Registry) (LiveConfig, error) {
	layout, err := self.BuildLayout(registry)
	if err != nil {
		return LiveConfig{}, err
	}
	snippetCommandSet, err := snippets.New(self.Snippets)
	if err != nil {
		path := self.getSourcePath("snippets")
		if path == "" {
			return LiveConfig{}, fmt.Errorf("snippets: %w", err)
		}
		return LiveConfig{}, fmt.Errorf("%s: snippets: %w", path, err)
	}
	permissions, err := self.Permissions.build()
	if err != nil {
		path := self.getSourcePath("permissions")
		if path == "" {
			return LiveConfig{}, err
		}
		return LiveConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return LiveConfig{
		ContinueMessage:    self.Input.Continue,
		EditorCommand:      self.Editor.Command,
		SegmentLayout:      layout,
		SnippetCommandSet:  snippetCommandSet,
		StreamingMode:      self.Ui.StreamingMode,
		Grouping:           self.Ui.Grouping,
		ReasoningRendering: self.Ui.ReasoningRendering,
		Theme:              self.Ui.Theme,
		ToolOutputBytes:    self.Tool.Output.Bytes,
		Permissions:        permissions,
		Experimental:       maps.Clone(self.Experimental),
		UnknownSettings:    self.UnknownSettings(),
	}, nil
}

func (self Bar) entries() map[segment.Position][]toml.Primitive {
	return map[segment.Position][]toml.Primitive{
		segment.TopLeft:      self.Top.Left,
		segment.TopCenter:    self.Top.Center,
		segment.TopRight:     self.Top.Right,
		segment.BottomLeft:   self.Bottom.Left,
		segment.BottomCenter: self.Bottom.Center,
		segment.BottomRight:  self.Bottom.Right,
	}
}

func (self Config) BuildLayout(registry segment.Registry) (segment.Layout, error) {
	return BuildLayout(registry, self.Bar, self.metaFor)
}

func BuildLayout(
	registry segment.Registry,
	writtenBar Bar,
	metaFor func(segment.Position) *toml.MetaData,
) (segment.Layout, error) {
	layout := segment.Layout{}

	for position, entries := range writtenBar.entries() {
		meta := metaFor(position)

		for _, entry := range entries {
			var namedFields struct {
				Segment string `toml:"segment"`
			}

			if err := meta.PrimitiveDecode(entry, &namedFields); err != nil {
				return nil, fmt.Errorf("%s: %w", position, err)
			}

			options := segmentOptions{meta: meta, entry: entry}

			builtSegment, err := registry.Build(namedFields.Segment, position, options)
			if err != nil {
				return nil, err
			}

			layout[position] = append(layout[position], segment.Instance{
				Name:    namedFields.Segment,
				Segment: builtSegment,
			})
		}
	}

	return layout, nil
}

type segmentOptions struct {
	meta  *toml.MetaData
	entry toml.Primitive
}

func (self segmentOptions) Read(into any) error {
	if err := self.meta.PrimitiveDecode(self.entry, into); err != nil {
		return err
	}

	return refuseUndrawableText(into)
}

func refuseUndrawableText(options any) error {
	value := reflect.ValueOf(options)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return nil
	}

	for field, setting := range value.Fields() {
		name := field.Tag.Get("toml")
		if name == "" {
			name = strings.ToLower(field.Name)
		}

		if err := refuseUndrawableValue(name, setting); err != nil {
			return err
		}
	}

	return nil
}

func refuseUndrawableValue(name string, setting reflect.Value) error {
	//nolint:exhaustive // every other kind holds no text
	switch setting.Kind() {
	case reflect.String:
		for _, character := range setting.String() {
			if unicode.IsControl(character) {
				return fmt.Errorf(
					"%s holds %q, which the terminal would read as an instruction rather than text",
					name, character,
				)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range setting.Len() {
			if err := refuseUndrawableValue(fmt.Sprintf("%s[%d]", name, i), setting.Index(i)); err != nil {
				return err
			}
		}
	default:
	}

	return nil
}

func (self Config) GetOverride() (Override, bool) {
	for _, source := range slices.Backward(self.sources) {
		if !source.source.IsOverride {
			continue
		}

		settings := make(map[string]struct{})
		for _, key := range source.meta.Keys() {
			if !source.meta.IsDefined(key...) || source.meta.Type(key...) == "Hash" || key.String() == "version" {
				continue
			}
			settings[leafTableOf(key).String()] = struct{}{}
		}
		return Override{Path: source.source.Path, Settings: slices.Sorted(maps.Keys(settings))}, true
	}
	return Override{}, false
}

func (self Config) UnknownSettings() []string {
	var reports []string

	for sourceIndex, source := range self.sources {
		unknown := source.meta.Undecoded()
		namedKeys := make([]string, 0, len(unknown))
		for _, key := range unknown {
			if self.isShadowed(sourceIndex, key) || isExperimental(key) {
				continue
			}
			namedKeys = append(namedKeys, key.String())
		}
		if len(namedKeys) == 0 {
			continue
		}

		slices.Sort(namedKeys)

		reports = append(reports, fmt.Sprintf("%s: unknown: %s", source.path, strings.Join(namedKeys, ", ")))
	}

	return append(reports, self.experimentalReports()...)
}

func (self Config) experimentalReports() []string {
	var reports []string

	for _, complaint := range experimental.Check(self.Experimental) {
		setting := "experimental." + complaint.Name
		if path := self.getSourcePath("experimental", complaint.Name); path != "" {
			setting = path + ": " + setting
		}
		reports = append(reports, fmt.Sprintf("%s: %s", setting, complaint.Reason))
	}

	return reports
}

func isExperimental(key toml.Key) bool {
	return len(key) > 0 && key[0] == "experimental"
}

func (self Config) isShadowed(sourceIndex int, key toml.Key) bool {
	if len(key) < 3 || key[0] != "bar" {
		return false
	}

	setting := key[:3]
	for _, source := range self.sources[sourceIndex+1:] {
		if source.meta.IsDefined(setting...) {
			return true
		}
	}
	return false
}

func (self Config) metaFor(position segment.Position) *toml.MetaData {
	side, end, _ := strings.Cut(position.String(), ".")

	for _, source := range slices.Backward(self.sources) {
		if source.meta.IsDefined("bar", side, end) {
			return source.meta
		}
	}

	return self.fallback
}

func (self Config) getSourcePath(keys ...string) string {
	for _, source := range slices.Backward(self.sources) {
		if source.meta.IsDefined(keys...) {
			return source.path
		}
	}
	return ""
}

func (self Config) getSourceFile(keys ...string) string {
	for _, source := range slices.Backward(self.sources) {
		if source.meta.IsDefined(keys...) {
			return source.source.Path
		}
	}
	return ""
}

func readConfigVersion(data []byte, isOverride bool) (int, error) {
	if isOverride {
		return Format, nil
	}

	version, err := format.ReadTOML(data)
	if err != nil {
		return 0, err
	}
	if version == 0 {
		return InitialFormat, nil
	}

	return version, nil
}

type Source struct {
	Path       string
	IsOverride bool
}

func Load(path string) (Config, error) {
	if path == "" {
		return loadSnapshots(nil)
	}
	return LoadSources(Source{Path: path})
}

func LoadSources(sources ...Source) (Config, error) {
	snapshots := make([]sourceSnapshot, 0, len(sources))
	for _, source := range sources {
		snapshots = append(snapshots, sourceSnapshot{source: source, snapshot: readSnapshot(source.Path)})
	}
	return loadSnapshots(snapshots)
}

type sourceSnapshot struct {
	source   Source
	snapshot snapshot
}

func loadSnapshots(sources []sourceSnapshot) (Config, error) {
	var config Config

	defaults, err := toml.Decode(defaultsTOML, &config)
	if err != nil {
		return config, fmt.Errorf("the built-in defaults are broken: %w", err)
	}

	config.fallback = &defaults

	for _, source := range sources {
		if source.snapshot.isMissing {
			continue
		}
		if err := applySnapshot(&config, source); err != nil {
			return config, err
		}
	}

	return config, nil
}

func applySnapshot(config *Config, source sourceSnapshot) error {
	displayPath := pathutil.Shorten(source.source.Path)

	if source.snapshot.failure != nil {
		return fmt.Errorf("%s: %w", displayPath, source.snapshot.failure)
	}

	version, err := readConfigVersion(source.snapshot.data, source.source.IsOverride)
	switch {
	case err != nil:
		return fmt.Errorf("%s: %w", displayPath, err)
	case version < Format:
		return fmt.Errorf("%s: config format %d needs migrating: run oh --ctl migrate", displayPath, version)
	}

	if err := format.Check(version, Format); err != nil {
		return fmt.Errorf("%s: config %w: upgrade oh", displayPath, err)
	}

	previousVersion := config.Version
	previousSnippets := maps.Clone(config.Snippets)
	meta, err := toml.Decode(string(source.snapshot.data), config)
	if err != nil {
		return fmt.Errorf("%s: %w", displayPath, err)
	}

	if source.source.IsOverride {
		config.Version = previousVersion
		if err := refuseWorkspaceSettings(meta); err != nil {
			return fmt.Errorf("%s: %w", displayPath, err)
		}
	}

	config.sources = append(config.sources, sourceMetadata{source: source.source, path: displayPath, meta: &meta})

	if meta.IsDefined("model", "round_robin") {
		if len(config.Model.RoundRobin) == 0 {
			return fmt.Errorf("%s: model.round_robin is empty, so there is nothing to ask", displayPath)
		}
		for _, selection := range config.Model.RoundRobin {
			if strings.TrimSpace(selection) == "" {
				return fmt.Errorf("%s: model.round_robin contains an empty selection", displayPath)
			}
		}
	}
	config.Provider.Ollama.Host = strings.TrimSpace(config.Provider.Ollama.Host)
	config.Ports.Hostname = strings.TrimSpace(config.Ports.Hostname)
	if err := validateHostSettings(
		config.Provider.Ollama.Host,
		meta.IsDefined("provider", "ollama", "host"),
		config.Ports.Hostname,
	); err != nil {
		return fmt.Errorf("%s: %w", displayPath, err)
	}
	config.Input.Continue = strings.TrimSpace(config.Input.Continue)
	if meta.IsDefined("input", "continue") && config.Input.Continue == "" {
		return fmt.Errorf("%s: input.continue is empty", displayPath)
	}
	if meta.IsDefined("tool", "output") && config.Tool.Output.Bytes < minimumToolOutputBytes {
		return fmt.Errorf(
			"%s: tool.output is too small to say anything with; write at least %d bytes",
			displayPath, minimumToolOutputBytes,
		)
	}
	for _, name := range slices.Sorted(maps.Keys(config.Snippets)) {
		if !meta.IsDefined("snippets", name) {
			continue
		}
		if previous, exists := previousSnippets[name]; exists && previous.File != "" {
			delete(config.snippetFileSnapshots, previous.File)
		}
		definition := config.Snippets[name]
		if definition.File != "" {
			resolvedPath, err := ResolveWrittenPath(source.source.Path, definition.File)
			if err != nil {
				return fmt.Errorf("%s: snippets.%s.file: %w", displayPath, name, err)
			}
			definition.File = resolvedPath
			config.Snippets[name] = definition

			current := readSnapshot(resolvedPath)
			if config.snippetFileSnapshots == nil {
				config.snippetFileSnapshots = make(map[string]snapshot)
			}
			config.snippetFileSnapshots[resolvedPath] = current
			if current.failure != nil {
				return fmt.Errorf(
					"%s: snippets.%s: could not read %s: %w",
					displayPath,
					name,
					resolvedPath,
					current.failure,
				)
			}
			if err := definition.LoadFileContents(resolvedPath, current.data); err != nil {
				return fmt.Errorf("%s: snippets.%s: %w", displayPath, name, err)
			}
		}
		config.Snippets[name] = definition
	}

	lists := []struct {
		name   string
		values *[]string
	}{
		{"skills.include", &config.Skills.Include},
		{"skills.exclude", &config.Skills.Exclude},
		{"sandbox.read", &config.Sandbox.Read},
		{"sandbox.write", &config.Sandbox.Write},
		{"sandbox.exec", &config.Sandbox.Exec},
		{"sandbox.path", &config.Sandbox.Path},
		{"sandbox.home", &config.Sandbox.Home},
	}
	for _, list := range lists {
		if !meta.IsDefined(strings.Split(list.name, ".")...) {
			continue
		}
		for i, writtenPath := range *list.values {
			resolvedPath, err := ResolveWrittenPath(source.source.Path, writtenPath)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", displayPath, list.name, err)
			}
			(*list.values)[i] = resolvedPath
		}
		*list.values = deduplicate(*list.values)
	}

	for _, mappedPath := range config.Sandbox.Home {
		if _, below := shell.HomeRelativePath(mappedPath); !below {
			return fmt.Errorf(
				"%s: sandbox.home: %s is not below the home directory, so it has nowhere to land",
				displayPath, mappedPath,
			)
		}
	}

	return nil
}

func deduplicate[Value comparable](values []Value) []Value {
	seenValues := make(map[Value]struct{}, len(values))
	result := make([]Value, 0, len(values))
	for _, value := range values {
		if _, exists := seenValues[value]; exists {
			continue
		}
		seenValues[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

var settingsAWorkspaceMayNotSet = []string{
	"editor",
	"experimental",
	"provider",
	"sandbox",
	"skills",
	toolsSetting,
}

func refuseWorkspaceSettings(meta toml.MetaData) error {
	for _, key := range meta.Keys() {
		if !meta.IsDefined(key...) || (meta.Type(key...) == "Hash" && len(key) < 2) {
			continue
		}

		if slices.Contains(settingsAWorkspaceMayNotSet, key[0]) {
			return fmt.Errorf(
				"%s cannot be overridden in oh.toml",
				key.String(),
			)
		}
	}

	return nil
}

func validateHostSettings(ollamaHost string, hasOllamaHost bool, hostname string) error {
	if hasOllamaHost && ollamaHost == "" {
		return errors.New("provider.ollama.host is empty")
	}
	if hostname == "" {
		return nil
	}
	if strings.Count(hostname, sessionPlaceholder) != 1 {
		return errors.New("ports.hostname must contain {session} exactly once")
	}
	return validateHostname(hostname)
}

func validateHostname(hostname string) error {
	if len(hostname) > hostnameLimit {
		return fmt.Errorf(
			"ports.hostname is %d characters long, and a hostname is at most %d",
			len(hostname), hostnameLimit,
		)
	}

	for _, character := range strings.ReplaceAll(hostname, sessionPlaceholder, "") {
		if !isHostnameCharacter(character) {
			return fmt.Errorf(
				"ports.hostname holds %q, and a hostname is letters, digits, dots, and dashes",
				character,
			)
		}
	}

	return nil
}

func isHostnameCharacter(character rune) bool {
	switch {
	case character >= 'a' && character <= 'z':
		return true
	case character >= 'A' && character <= 'Z':
		return true
	case character >= '0' && character <= '9':
		return true
	default:
		return character == '.' || character == '-'
	}
}

func ResolveWrittenPath(sourcePath string, writtenPath string) (string, error) {
	if writtenPath == "" {
		return "", errors.New("path is empty")
	}

	path, err := pathutil.Expand(writtenPath)
	if err != nil {
		return "", fmt.Errorf("could not expand %q: %w", writtenPath, err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(sourcePath), path)
	}

	return filepath.Clean(path), nil
}
