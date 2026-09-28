package store

import "crdx.org/oh/pkg/tool"

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Revision    string          `json:"revision"`
	Parameters  []ToolParameter `json:"parameters,omitempty"`
}

type ToolParameter struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	ItemType    string   `json:"item_type,omitempty"`
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	IsOptional  bool     `json:"optional,omitempty"`
}

func FreezeTools(tools []tool.Tool) []ToolDefinition {
	frozenDefinitions := make([]ToolDefinition, len(tools))

	for at, availableTool := range tools {
		snapshot := tool.TakeSnapshot(availableTool)
		parameters := make([]ToolParameter, len(snapshot.Definition.Schema))
		for index, parameter := range snapshot.Definition.Schema {
			parameters[index] = ToolParameter{
				Name:        parameter.Name,
				Type:        string(parameter.Type),
				ItemType:    string(parameter.ItemType),
				Description: parameter.Description,
				Values:      parameter.Values,
				IsOptional:  parameter.IsOptional(),
			}
		}

		frozenDefinitions[at] = ToolDefinition{
			Name:        snapshot.Definition.Name,
			Description: snapshot.Definition.Description,
			Revision:    snapshot.Revision,
			Parameters:  parameters,
		}
	}

	return frozenDefinitions
}

func RestoreTools(frozenDefinitions []ToolDefinition) []tool.Snapshot {
	snapshots := make([]tool.Snapshot, len(frozenDefinitions))

	for at, frozenDefinition := range frozenDefinitions {
		schema := make(tool.Schema, len(frozenDefinition.Parameters))
		for index, frozenParameter := range frozenDefinition.Parameters {
			parameter := tool.Parameter{
				Name:        frozenParameter.Name,
				Type:        tool.DataType(frozenParameter.Type),
				ItemType:    tool.DataType(frozenParameter.ItemType),
				Description: frozenParameter.Description,
				Values:      frozenParameter.Values,
			}
			if frozenParameter.IsOptional {
				parameter = parameter.Optional()
			}
			schema[index] = parameter
		}

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
