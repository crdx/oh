package tool

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/util/strutil"
)

type Arguments struct {
	schema Schema
	values map[string]value
}

type value struct {
	text   string
	number int64
	flag   bool
	list   []string
}

func (self Schema) Decode(text string) (Arguments, error) {
	text = strings.TrimSpace(text)
	fields := map[string]json.RawMessage{}

	if text != "" {
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			return Arguments{}, fmt.Errorf("could not parse the arguments: %w", err)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if self.Find(name) == nil {
			return Arguments{}, fmt.Errorf("%s is not a parameter of this tool", name)
		}
	}

	arguments := Arguments{schema: self, values: make(map[string]value, len(self))}

	for _, parameter := range self {
		raw, isSupplied := fields[parameter.Name]
		if isSupplied && string(raw) == "null" {
			isSupplied = false
		}

		if !isSupplied {
			if !parameter.isOptional {
				return Arguments{}, fmt.Errorf("%s is required", parameter.Name)
			}

			continue
		}

		content, err := parameter.decode(raw)
		if err != nil {
			return Arguments{}, err
		}

		arguments.values[parameter.Name] = content
	}

	return arguments, nil
}

func (self Schema) Find(name string) *Parameter {
	for index, parameter := range self {
		if parameter.Name == name {
			return &self[index]
		}
	}

	return nil
}

func (self Parameter) decode(raw json.RawMessage) (value, error) {
	var content value

	switch self.Type {
	case TypeString:
		if err := json.Unmarshal(raw, &content.text); err != nil {
			return value{}, self.refuse("a string", raw)
		}
		if len(self.Values) > 0 && !slices.Contains(self.Values, content.text) {
			return value{}, fmt.Errorf("%s must be one of: %s", self.Name, strings.Join(self.Values, ", "))
		}
	case TypeInteger:
		if err := json.Unmarshal(raw, &content.number); err != nil {
			return value{}, self.refuse("an integer", raw)
		}
	case TypeBoolean:
		if err := json.Unmarshal(raw, &content.flag); err != nil {
			return value{}, self.refuse("true or false", raw)
		}
	case TypeArray:
		if self.ItemType != TypeString {
			return value{}, fmt.Errorf("%s holds %s, which cannot be decoded", self.Name, self.ItemType)
		}
		if err := json.Unmarshal(raw, &content.list); err != nil {
			return value{}, self.refuse("a list of strings", raw)
		}
	case TypeObject:
		return value{}, fmt.Errorf("%s is an object, which cannot be decoded", self.Name)
	}

	return content, nil
}

func (self Parameter) refuse(shape string, raw json.RawMessage) error {
	return fmt.Errorf("%s must be %s, not %s", self.Name, shape, strutil.FirstLine(string(raw)))
}

func (self Arguments) Schema() Schema {
	return self.schema
}

func (self Arguments) IsPresent(name string) bool {
	self.require(name)
	_, isPresent := self.values[name]

	return isPresent
}

func (self Arguments) GetString(name string) string {
	return self.get(name, TypeString).text
}

func (self Arguments) GetInteger(name string) int64 {
	return self.get(name, TypeInteger).number
}

func (self Arguments) GetBoolean(name string) bool {
	return self.get(name, TypeBoolean).flag
}

func (self Arguments) GetStrings(name string) []string {
	return self.get(name, TypeArray).list
}

func (self Arguments) GetText(name string) string {
	parameter := self.require(name)
	content := self.values[name]

	switch parameter.Type {
	case TypeString:
		return content.text
	case TypeInteger:
		return strconv.FormatInt(content.number, 10)
	case TypeBoolean:
		return strconv.FormatBool(content.flag)
	case TypeArray:
		return strings.Join(content.list, " ")
	case TypeObject:
		return ""
	}

	return ""
}

func (self Arguments) get(name string, dataType DataType) value {
	if parameter := self.require(name); parameter.Type != dataType {
		panic(name + " is " + string(parameter.Type) + ", not " + string(dataType))
	}

	return self.values[name]
}

func (self Arguments) require(name string) Parameter {
	parameter := self.schema.Find(name)
	if parameter == nil {
		panic("no parameter named " + name)
	}

	return *parameter
}
