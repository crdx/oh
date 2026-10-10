package store

import "crdx.org/oh/pkg/tool"

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Revision    string          `json:"revision"`
	Parameters  []ToolParameter `json:"parameters,omitempty"`
}

type ToolParameter struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	ItemType    string          `json:"item_type,omitempty"`
	ItemFields  []ToolParameter `json:"item_fields,omitempty"`
	Description string          `json:"description"`
	Values      []string        `json:"values,omitempty"`
	IsOptional  bool            `json:"optional,omitempty"`
}

func FreezeTools(tools []tool.Tool) []ToolDefinition {
	frozenDefinitions := make([]ToolDefinition, len(tools))

	for at, availableTool := range tools {
		snapshot := tool.TakeSnapshot(availableTool)
		parameters := freezeParameters(snapshot.Definition.Schema)

		frozenDefinitions[at] = ToolDefinition{
			Name:        snapshot.Definition.Name,
			Description: snapshot.Definition.Description,
			Revision:    snapshot.Revision,
			Parameters:  parameters,
		}
	}

	return frozenDefinitions
}

func freezeParameters(schema tool.Schema) []ToolParameter {
	if len(schema) == 0 {
		return nil
	}
	parameters := make([]ToolParameter, len(schema))
	for index, parameter := range schema {
		parameters[index] = ToolParameter{
			Name:        parameter.Name,
			Type:        string(parameter.Type),
			ItemType:    string(parameter.ItemType),
			ItemFields:  freezeParameters(parameter.ItemSchema),
			Description: parameter.Description,
			Values:      parameter.Values,
			IsOptional:  parameter.IsOptional(),
		}
	}
	return parameters
}

func RestoreTools(frozenDefinitions []ToolDefinition) []tool.Snapshot {
	snapshots := make([]tool.Snapshot, len(frozenDefinitions))

	for at, frozenDefinition := range frozenDefinitions {
		schema := restoreParameters(frozenDefinition.Parameters)

		snapshots[at] = tool.Snapshot{
			Definition: tool.Definition{
				Name:        frozenDefinition.Name,
				Description: frozenDefinition.Description,
				Schema:      schema,
			},
			Revision: frozenDefinition.Revision,
		}
	}

	return snapshots
}

func restoreParameters(frozenParameters []ToolParameter) tool.Schema {
	schema := make(tool.Schema, len(frozenParameters))
	for index, field := range frozenParameters {
		parameter := tool.Parameter{
			Name:        field.Name,
			Type:        tool.DataType(field.Type),
			ItemType:    tool.DataType(field.ItemType),
			ItemSchema:  restoreParameters(field.ItemFields),
			Description: field.Description,
			Values:      field.Values,
		}
		if field.IsOptional {
			parameter = parameter.Optional()
		}
		schema[index] = parameter
	}
	return schema
}
