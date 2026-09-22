package world

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/pkg/tool/command"
	"crdx.org/oh/pkg/toolbox"
)

const toolsSetting = "tools"

var sandboxBoundTools = []string{"bash", "job", "expose"}

type World struct {
	Path   string                       `toml:"-"`
	Prompt Prompt                       `toml:"prompt"`
	Files  Files                        `toml:"files"`
	Tools  map[string]config.CustomTool `toml:"tools"`
}

type Files struct {
	Root       string `toml:"root"`
	IsWritable bool   `toml:"writable"`
}

type Prompt struct {
	Text string
	File string
}

func (self *Prompt) UnmarshalTOML(value any) error {
	switch contents := value.(type) {
	case string:
		self.Text = strings.TrimSpace(contents)
		if self.Text == "" {
			return errors.New("empty")
		}

		return nil
	case map[string]any:
		path, isPath := contents["file"].(string)
		if !isPath || strings.TrimSpace(path) == "" {
			return errors.New("table requires a file key")
		}
		self.File = path

		return nil
	default:
		return errors.New("expected a string or a table with a file key")
	}
}

func Load(path string) (World, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return World{}, fmt.Errorf("could not resolve %s: %w", path, err)
	}

	data, err := config.ReadWrittenFile(absolutePath)
	if err != nil {
		return World{}, err
	}

	contents := World{Path: absolutePath}

	meta, err := toml.Decode(string(data), &contents)
	if err != nil {
		return World{}, err
	}

	if err := refuseUnknownSettings(meta); err != nil {
		return World{}, err
	}

	if err := contents.checkTools(); err != nil {
		return World{}, err
	}

	if contents.Prompt.Text, err = readPrompt(absolutePath, contents.Prompt); err != nil {
		return World{}, err
	}

	if contents.Files.Root, err = resolveRoot(absolutePath, contents.Files.Root, contents.pathToolNames()); err != nil {
		return World{}, err
	}

	return contents, nil
}

func (self World) OfferedNames() []string {
	return slices.Sorted(maps.Keys(self.Tools))
}

func (self World) ReferenceNames() []string {
	var names []string

	for _, name := range self.OfferedNames() {
		if self.Tools[name].IsReference() {
			names = append(names, name)
		}
	}

	return names
}

func (self World) Declarations() ([]command.Declaration, error) {
	var declarations []command.Declaration

	for _, name := range self.OfferedNames() {
		if self.Tools[name].IsReference() {
			continue
		}

		declaration, err := config.Declare(name, self.Tools[name], self.Path)
		if err != nil {
			return nil, self.complain(name, err)
		}

		declarations = append(declarations, declaration)
	}

	return declarations, nil
}

func readPrompt(sourcePath string, prompt Prompt) (string, error) {
	if prompt.File != "" {
		path, err := config.ResolveWrittenPath(sourcePath, prompt.File)
		if err != nil {
			return "", fmt.Errorf("prompt.file: %w", err)
		}

		data, err := config.ReadWrittenFile(path)
		if err != nil {
			return "", fmt.Errorf("prompt.file: %s: %w", path, err)
		}

		prompt.Text = strings.TrimSpace(string(data))
	}

	if prompt.Text == "" {
		return "", errors.New("prompt: required")
	}

	return prompt.Text, nil
}

func resolveRoot(sourcePath string, writtenRoot string, pathToolNames []string) (string, error) {
	if writtenRoot == "" {
		if len(pathToolNames) > 0 {
			return "", fmt.Errorf(
				"files.root: required by %s",
				strings.Join(pathToolNames, ", "),
			)
		}

		return "", nil
	}

	path, err := config.ResolveWrittenPath(sourcePath, writtenRoot)
	if err != nil {
		return "", fmt.Errorf("files.root: %w", err)
	}

	return path, nil
}

func (self World) pathToolNames() []string {
	var names []string

	for _, name := range self.OfferedNames() {
		if self.Tools[name].IsReference() && slices.Contains(toolbox.PathToolNames, name) {
			names = append(names, name)
		}
	}

	return names
}

func (self World) checkTools() error {
	for _, name := range self.OfferedNames() {
		if slices.Contains(sandboxBoundTools, name) {
			return self.complain(name, errors.New("sandbox unavailable"))
		}

		if err := self.Tools[name].CheckReference(); err != nil {
			return self.complain(name, err)
		}
	}

	return nil
}

func (self World) complain(name string, err error) error {
	return fmt.Errorf("%s.%s: %w", toolsSetting, name, err)
}

func refuseUnknownSettings(meta toml.MetaData) error {
	unknown := meta.Undecoded()
	if len(unknown) == 0 {
		return nil
	}

	names := make([]string, 0, len(unknown))
	for _, key := range unknown {
		names = append(names, key.String())
	}
	slices.Sort(names)

	return fmt.Errorf("unknown: %s", strings.Join(names, ", "))
}
