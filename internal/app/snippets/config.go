package snippets

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type ArgumentPolicy string

const (
	ArgumentsRequired ArgumentPolicy = "required"
	ArgumentsOptional ArgumentPolicy = "optional"
	ArgumentsNone     ArgumentPolicy = "none"
)

type Definition struct {
	Prompt      string
	File        string
	Description string
	Arguments   ArgumentPolicy
}

func (self *Definition) UnmarshalTOML(value any) error {
	switch configuredValue := value.(type) {
	case string:
		self.Prompt = strings.TrimSpace(configuredValue)
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

func (self *Definition) LoadFileContents(path string, contents []byte) error {
	self.File = path
	self.Prompt = strings.TrimSpace(string(contents))
	if self.Prompt == "" {
		return errors.New("prompt file is empty")
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

	var err error
	if self.Prompt, err = getString(configuredTable, "prompt"); err != nil {
		return err
	}
	if self.File, err = getString(configuredTable, "file"); err != nil {
		return err
	}
	if self.Description, err = getString(configuredTable, "description"); err != nil {
		return err
	}
	arguments, err := getString(configuredTable, "arguments")
	if err != nil {
		return err
	}
	self.Arguments = ArgumentPolicy(arguments)

	if (self.Prompt == "") == (self.File == "") {
		return errors.New("set exactly one of prompt or file")
	}
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

func getString(configuredTable map[string]any, name string) (string, error) {
	value, isFound := configuredTable[name]
	if !isFound {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", name)
	}
	return strings.TrimSpace(text), nil
}
