package snippets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"crdx.org/oh/internal/util/frontmatter"
	"crdx.org/oh/internal/util/pathutil"
)

type ArgumentPolicy string

const (
	ArgumentsRequired ArgumentPolicy = "required"
	ArgumentsOptional ArgumentPolicy = "optional"
	ArgumentsNone     ArgumentPolicy = "none"
)

const onePromptSource = "set exactly one of prompt or file"

type Definition struct {
	Prompt      string
	File        string
	Description string
	Arguments   ArgumentPolicy
}

func (self *Definition) UnmarshalTOML(value any) error {
	switch configuredValue := value.(type) {
	case string:
		*self = Definition{Prompt: strings.TrimSpace(configuredValue)}
		if self.Prompt == "" {
			return errors.New("prompt is empty")
		}
		return nil
	case map[string]any:
		return self.unmarshalTable(configuredValue)
	default:
		return errors.New("definition is not a prompt or a table")
	}
}

type fileMetadata struct {
	Description string         `yaml:"description"`
	Arguments   ArgumentPolicy `yaml:"arguments"`
}

func (self *Definition) LoadFileContents(path string, contents []byte) error {
	header, body, _, err := frontmatter.Split(contents)
	if err != nil {
		return err
	}

	var metadata fileMetadata
	decoder := yaml.NewDecoder(bytes.NewReader(header))
	decoder.KnownFields(true)
	if err := decoder.Decode(&metadata); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("could not parse frontmatter: %w", err)
	}

	self.File = path
	self.Prompt = strings.TrimSpace(string(body))
	if self.Prompt == "" {
		return errors.New("prompt file is empty")
	}
	if self.Description == "" {
		self.Description = strings.TrimSpace(metadata.Description)
	}
	if self.Arguments == "" {
		self.Arguments = metadata.Arguments
	}
	return self.validate()
}

func (self *Definition) CheckSource() error {
	if self.Prompt == "" && self.File == "" {
		return errors.New(onePromptSource)
	}
	return nil
}

func (self *Definition) unmarshalTable(configuredTable map[string]any) error {
	knownFields := map[string]bool{
		"arguments":   true,
		"description": true,
		"file":        true,
		"prompt":      true,
	}
	var unknown []string
	for name := range configuredTable {
		if !knownFields[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("unknown: %s", strings.Join(unknown, ", "))
	}

	_, hasPrompt := configuredTable["prompt"]
	_, hasFile := configuredTable["file"]
	if hasPrompt && hasFile {
		return errors.New(onePromptSource)
	}
	if hasPrompt {
		self.File = ""
		if err := setString(configuredTable, "prompt", &self.Prompt); err != nil {
			return err
		}
	}
	if hasFile {
		self.Prompt = ""
		if err := setString(configuredTable, "file", &self.File); err != nil {
			return err
		}
	}
	if err := setString(configuredTable, "description", &self.Description); err != nil {
		return err
	}
	arguments := string(self.Arguments)
	if err := setString(configuredTable, "arguments", &arguments); err != nil {
		return err
	}
	self.Arguments = ArgumentPolicy(arguments)
	return self.validate()
}

func (self *Definition) validate() error {
	if strings.ContainsAny(self.Description, "\r\n") {
		return errors.New("description must fit on one line")
	}
	switch self.Arguments {
	case ArgumentsRequired, ArgumentsOptional, ArgumentsNone, "":
		return nil
	default:
		return fmt.Errorf("arguments is %q, want required, optional, or none", self.Arguments)
	}
}

func (self *Definition) subject(name string) string {
	if self.File == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, pathutil.Shorten(self.File))
}

func setString(configuredTable map[string]any, name string, target *string) error {
	value, isFound := configuredTable[name]
	if !isFound {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s is not a string", name)
	}
	*target = strings.TrimSpace(text)
	return nil
}
