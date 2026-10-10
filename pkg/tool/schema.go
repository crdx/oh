package tool

import (
	"encoding/json"
	"slices"
	"strings"

	"crdx.org/oh/internal/util/strutil"
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
	ItemSchema  Schema
	Description string
	Values      []string

	isOptional bool
}

func (self Parameter) Optional() Parameter {
	self.isOptional = true
	return self
}

func (self Parameter) IsOptional() bool {
	return self.isOptional
}

func String(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeString, Description: description}
}

func StringArray(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeArray, ItemType: TypeString, Description: description}
}

func ObjectArray(name string, description string, fields Schema) Parameter {
	return Parameter{Name: name, Type: TypeArray, ItemType: TypeObject, ItemSchema: fields, Description: description}
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
	Type                 DataType            `json:"type"`
	Properties           map[string]property `json:"properties,omitempty"`
	RequiredNames        []string            `json:"required,omitempty"`
	AdditionalProperties *bool               `json:"additionalProperties,omitempty"`
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
			if parameter.ItemType == TypeObject {
				var allowsAdditionalProperties bool
				renderedProperty.Items.AdditionalProperties = &allowsAdditionalProperties
				properties := make(map[string]property, len(parameter.ItemSchema))
				for _, field := range parameter.ItemSchema {
					properties[field.Name] = property{Type: field.Type, Description: field.Description}
					if !field.IsOptional() {
						renderedProperty.Items.RequiredNames = append(renderedProperty.Items.RequiredNames, field.Name)
					}
				}
				renderedProperty.Items.Properties = properties
			}
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

type Snapshot struct {
	Definition Definition
	Revision   string
}

func Describe(subject Tool) Definition {
	return cloneDefinition(Definition{
		Name:        subject.Name(),
		Description: subject.Description(),
		Schema:      subject.Schema(),
	})
}

func TakeSnapshot(subject Tool) Snapshot {
	return Snapshot{Definition: Describe(subject), Revision: subject.Revision()}
}

type compatibilityReporter interface {
	CompatibleWith(revision string) bool
}

func AcceptsRevision(subject Tool, revision string) bool {
	reporter, isReported := subject.(compatibilityReporter)
	return isReported && reporter.CompatibleWith(revision)
}

func IsCompatible(subject Tool, snapshot Snapshot) bool {
	return subject.Revision() == snapshot.Revision || AcceptsRevision(subject, snapshot.Revision)
}

func cloneDefinition(definition Definition) Definition {
	clonedSchema := make(Schema, len(definition.Schema))
	for i, parameter := range definition.Schema {
		parameter.Values = slices.Clone(parameter.Values)
		parameter.ItemSchema = cloneDefinition(Definition{Schema: parameter.ItemSchema}).Schema
		clonedSchema[i] = parameter
	}
	definition.Schema = clonedSchema
	return definition
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
