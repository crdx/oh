package jobs

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/sandbox"
)

func TestJobNamesUseTenLowercaseLettersDigitsAndHyphens(t *testing.T) {
	testCases := []struct {
		name    string
		isValid bool
	}{
		{name: "a", isValid: true},
		{name: "abc123", isValid: true},
		{name: "abcdefghij", isValid: true},
		{name: "doc-server", isValid: true},
		{name: "-", isValid: true},
		{name: ""},
		{name: "abcdefghijk"},
		{name: "Job"},
		{name: "job_name"},
		{name: "two words"},
		{name: "é"},
	}

	for _, testCase := range testCases {
		err := ValidateName(testCase.name)
		if (err == nil) != testCase.isValid {
			t.Errorf("ValidateName(%q) gave %v, want validity %v", testCase.name, err, testCase.isValid)
		}
	}
}

func TestTheManagerRefusesAnInvalidNameBeforeStarting(t *testing.T) {
	_, err := New(nil).Start(t.Context(), "Job", t.TempDir(), "true", sandbox.Policy{})
	if err == nil || !strings.Contains(err.Error(), "[a-z0-9-]") {
		t.Errorf("got %v, want the job name requirements", err)
	}
}

func TestARestoredLegacyNameRemainsAddressable(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "legacy-name", State: StateComplete}})

	if _, err := manager.Status("legacy-name"); err != nil {
		t.Errorf("got %v, want the restored job to remain addressable", err)
	}
}
