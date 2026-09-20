package tool

import (
	"encoding/json"
	"strings"

	"crdx.org/io/internal/util/strutil"
)

type DataType string

const (
	TypeObject  DataType = "object"
	TypeArray   DataType = "array"
	TypeString  DataType = "string"
	TypeInteger DataType = "integer"
	TypeBoolean DataType = "boolean"
)

type Schema []Parameter

type Parameter struct {
	Name        string
	Type        DataType
	ItemType    DataType
	Description string
	Values      []string

	isOptional bool
}

func (self Parameter) Optional() Parameter {
	self.isOptional = true
	return self
}

func String(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeString, Description: description}
}

func StringArray(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeArray, ItemType: TypeString, Description: description}
}

func Integer(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeInteger, Description: description}
}

func Boolean(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeBoolean, Description: description}
}

func Enum(name string, description string, values ...string) Parameter {
	return Parameter{Name: name, Type: TypeString, Description: description, Values: values}
}

type item struct {
	Type DataType `json:"type"`
}

type property struct {
	Type        DataType `json:"type"`
	Description string   `json:"description"`
	Values      []string `json:"enum,omitempty"`
	Items       *item    `json:"items,omitempty"`
}

type object struct {
	Type                 DataType            `json:"type"`
	Properties           map[string]property `json:"properties"`
	RequiredNames        []string            `json:"required,omitempty"`
	AdditionalProperties bool                `json:"additionalProperties"`
}

func (self Schema) MarshalJSON() ([]byte, error) {
	renderedSchema := object{
		Type:       TypeObject,
		Properties: make(map[string]property, len(self)),
	}

	for _, parameter := range self {
		renderedProperty := property{
			Type:        parameter.Type,
			Description: parameter.Description,
			Values:      parameter.Values,
		}
		if parameter.ItemType != "" {
			renderedProperty.Items = &item{Type: parameter.ItemType}
		}
		renderedSchema.Properties[parameter.Name] = renderedProperty

		if !parameter.isOptional {
			renderedSchema.RequiredNames = append(renderedSchema.RequiredNames, parameter.Name)
		}
	}

	return json.Marshal(renderedSchema)
}

type Definition struct {
	Name        string
	Description string
	Schema      Schema
}

func Describe(subject Tool) Definition {
	return Definition{
		Name:        subject.Name(),
		Description: subject.Description(),
		Schema:      subject.Schema(),
	}
}

func DescribeUnparsedArguments(subject Tool, arguments string) string {
	var decodedFields map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &decodedFields) != nil {
		return strutil.FirstLine(arguments)
	}

	var values []string
	knownParameterCount := 0

	for _, parameter := range subject.Schema() {
		raw, isPresent := decodedFields[parameter.Name]
		if !isPresent {
			continue
		}
		knownParameterCount++

		var text string
		if json.Unmarshal(raw, &text) != nil {
			text = string(raw)
		}

		if text = strings.TrimSpace(text); text != "" {
			values = append(values, text)
		}
	}

	if len(values) > 0 {
		return strutil.FirstLine(strings.Join(values, " "))
	}
	if decodedFields != nil && knownParameterCount == len(decodedFields) {
		return ""
	}

	return strutil.FirstLine(arguments)
}
