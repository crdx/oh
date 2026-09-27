package mermaid

import (
	"strings"
	"testing"
)

func TestHarnessPromptOnlyNamedDiagramsDraw(t *testing.T) {
	for _, source := range []string{
		"stateDiagram-v2\nA --> B",
		"classDiagram\nA <|-- B",
		"pie\n\"a\": 1",
		"gantt\ntitle Plan",
		"journey\ntitle Day",
		"gitGraph\ncommit",
		"mindmap\nroot",
		"timeline\ntitle History",
		"quadrantChart\ntitle Quadrants",
	} {
		if _, err := Render(source); err == nil {
			t.Errorf("expected %q to fail", source)
		}
	}
}

func TestHarnessPromptOnlyRectangleNodesDraw(t *testing.T) {
	for _, node := range []string{
		"A(round)",
		"A([stadium])",
		"A((circle))",
		"A(((double)))",
		"A{diamond}",
		"A{{hexagon}}",
		"A>flag]",
	} {
		source := "graph LR\n" + node + " --> B"
		if _, err := Render(source); err == nil {
			t.Errorf("expected %q to fail", source)
		}
	}
}

func TestHarnessPromptOnlyArrowEdgesDraw(t *testing.T) {
	for _, edge := range []string{
		"A --- B",
		"A -.-> B",
		"A ==> B",
		"A -- text --> B",
		"A --o B",
		"A --x B",
		"A <-- B",
		"A ~~~ B",
	} {
		source := "graph LR\n" + edge
		if _, err := Render(source); err == nil {
			t.Errorf("expected %q to fail", source)
		}
	}
}

func TestHarnessPromptReversedDirectionsDrawForwards(t *testing.T) {
	for reversed, forwards := range map[string]string{
		"BT": "TD",
		"RL": "LR",
	} {
		reversedOutput, err := Render("graph " + reversed + "\nA --> B")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", reversed, err)
		}
		forwardsOutput, err := Render("graph " + forwards + "\nA --> B")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", forwards, err)
		}
		if reversedOutput != forwardsOutput {
			t.Errorf("expected %s to draw as %s:\n%s\n%s", reversed, forwards, reversedOutput, forwardsOutput)
		}
	}
}

func TestHarnessPromptFlowchartStylingDrawsNothing(t *testing.T) {
	plainOutput, err := Render("graph LR\nA --> B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	styledOutput, err := Render("graph LR\nclassDef hot color:#ff0000\nA:::hot --> B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if styledOutput != plainOutput {
		t.Errorf("expected styling to draw nothing:\n%s\n%s", styledOutput, plainOutput)
	}
}

func TestHarnessPromptFlowchartExtrasFail(t *testing.T) {
	for _, line := range []string{
		"style A fill:#f00",
		"class A hot",
		"linkStyle 0 stroke:#f00",
		"click A https://example.com",
		"direction TB",
	} {
		source := "graph LR\nA --> B\n" + line
		if _, err := Render(source); err == nil {
			t.Errorf("expected %q to fail", source)
		}
	}
}

func TestHarnessPromptSequenceExtrasFail(t *testing.T) {
	for _, line := range []string{
		"activate B",
		"deactivate B",
		"title Greeting",
		"box Aqua Group",
		"create participant C",
		"destroy B",
	} {
		source := "sequenceDiagram\nA->>B: hello\n" + line
		if _, err := Render(source); err == nil {
			t.Errorf("expected %q to fail", source)
		}
	}
}

func TestHarnessPromptSequenceActivationMarkersNameParticipants(t *testing.T) {
	output, err := Render("sequenceDiagram\nA->>+B: hello\nB-->>-A: goodbye")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, participant := range []string{"+B", "-A"} {
		if !strings.Contains(output, participant) {
			t.Errorf("expected %q drawn as a participant in:\n%s", participant, output)
		}
	}
}
