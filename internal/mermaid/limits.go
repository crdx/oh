package mermaid

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"crdx.org/oh/internal/mermaid/er"
	"crdx.org/oh/internal/mermaid/runewidth"
	"crdx.org/oh/internal/mermaid/sequence"
)

const (
	maximumSourceBytes          = 32 * 1024
	maximumSourceLines          = 512
	maximumLineCells            = 512
	maximumFlowchartNodes       = 128
	maximumFlowchartEdges       = 256
	maximumFlowchartSubgraphs   = 64
	maximumLabelLines           = 32
	maximumSequenceParticipants = 64
	maximumSequenceEvents       = 256
	maximumEntities             = 64
	maximumRelationships        = 256
	maximumAttributes           = 256
	maximumCanvasColumns        = 8192
	maximumCanvasRows           = 8192
	maximumCanvasCells          = 1 << 21
)

func validateSourceLimits(source string) error {
	if !utf8.ValidString(source) {
		return errors.New("source is not valid UTF-8")
	}
	if len(source) > maximumSourceBytes {
		return fmt.Errorf("source exceeds %d bytes", maximumSourceBytes)
	}

	lineCount := 0
	for line := range strings.SplitSeq(source, "\n") {
		lineCount++
		if lineCount > maximumSourceLines {
			return fmt.Errorf("source exceeds %d lines", maximumSourceLines)
		}
		if width := runewidth.StringWidth(line); width > maximumLineCells {
			return fmt.Errorf("line %d exceeds %d cells", lineCount, maximumLineCells)
		}
	}

	return nil
}

func validateFlowchartLimits(properties *graphProperties) error {
	nodeCount := 0
	edgeCount := 0
	for element := properties.data.Front(); element != nil; element = element.Next() {
		nodeCount++
		edgeCount += len(element.Value)
	}

	switch {
	case nodeCount > maximumFlowchartNodes:
		return fmt.Errorf("flowchart exceeds %d nodes", maximumFlowchartNodes)
	case edgeCount > maximumFlowchartEdges:
		return fmt.Errorf("flowchart exceeds %d edges", maximumFlowchartEdges)
	case len(properties.subgraphs) > maximumFlowchartSubgraphs:
		return fmt.Errorf("flowchart exceeds %d subgraphs", maximumFlowchartSubgraphs)
	}

	for _, specification := range properties.nodeSpecs {
		if len(specification.label.lines) > maximumLabelLines {
			return fmt.Errorf("flowchart label exceeds %d lines", maximumLabelLines)
		}
	}
	for _, subgraph := range properties.subgraphs {
		if len(subgraph.label.lines) > maximumLabelLines {
			return fmt.Errorf("subgraph label exceeds %d lines", maximumLabelLines)
		}
	}

	return nil
}

func validateSequenceLimits(parsedDiagram *sequence.SequenceDiagram) error {
	if len(parsedDiagram.Participants) > maximumSequenceParticipants {
		return fmt.Errorf("sequence diagram exceeds %d participants", maximumSequenceParticipants)
	}
	if len(parsedDiagram.Events) > maximumSequenceEvents {
		return fmt.Errorf("sequence diagram exceeds %d events", maximumSequenceEvents)
	}

	columns := 0
	for _, participant := range parsedDiagram.Participants {
		columns += runewidth.StringWidth(participant.Label) + 8
	}
	if columns > maximumCanvasColumns {
		return fmt.Errorf("sequence diagram exceeds %d columns", maximumCanvasColumns)
	}

	return nil
}

func validateEntityRelationshipLimits(parsedDiagram *er.ErDiagram) error {
	if len(parsedDiagram.Entities) > maximumEntities {
		return fmt.Errorf("entity-relationship diagram exceeds %d entities", maximumEntities)
	}
	if len(parsedDiagram.Relationships) > maximumRelationships {
		return fmt.Errorf("entity-relationship diagram exceeds %d relationships", maximumRelationships)
	}

	attributeCount := 0
	for _, entity := range parsedDiagram.Entities {
		attributeCount += len(entity.Attributes)
	}
	if attributeCount > maximumAttributes {
		return fmt.Errorf("entity-relationship diagram exceeds %d attributes", maximumAttributes)
	}

	return nil
}

func validateCanvasLimits(columns int, rows int) error {
	if columns > maximumCanvasColumns {
		return fmt.Errorf("diagram exceeds %d columns", maximumCanvasColumns)
	}
	if rows > maximumCanvasRows {
		return fmt.Errorf("diagram exceeds %d rows", maximumCanvasRows)
	}
	if rows > 0 && columns > maximumCanvasCells/rows {
		return fmt.Errorf("diagram exceeds %d cells", maximumCanvasCells)
	}
	return nil
}
