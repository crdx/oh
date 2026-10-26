package cli

import "testing"

func TestInProcessHeadlessInputCheckRefusesAnInteractiveTool(t *testing.T) {
	input := Input{inputFlags: inputFlags{IsPrinting: true, Tools: []string{"read", "task"}}}

	err := input.Check(false)
	if err == nil || err.Error() != "a headless session offers only read, ls, find, grep, bash, write, edit, so it cannot offer task" {
		t.Errorf("got %v", err)
	}
}
