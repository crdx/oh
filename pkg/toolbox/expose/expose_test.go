package expose

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type recordedPorts struct {
	open    []uint16
	refusal error
}

func (self *recordedPorts) Expose(port uint16) (string, error) {
	if self.refusal != nil {
		return "", self.refusal
	}
	self.open = append(self.open, port)

	return "http://127.9.9.9:" + strconv.Itoa(int(port)), nil
}

func (self *recordedPorts) Hide(port uint16) error {
	if self.refusal != nil {
		return self.refusal
	}
	self.open = slices.DeleteFunc(self.open, func(openPort uint16) bool { return openPort == port })

	return nil
}

func (self *recordedPorts) List() []Publication {
	publications := make([]Publication, 0, len(self.open))
	for _, port := range self.open {
		publications = append(publications, Publication{
			Port: port,
			URL:  "http://127.9.9.9:" + strconv.Itoa(int(port)),
		})
	}

	return publications
}

func TestExposingSaysWhereTheUserCanOpenIt(t *testing.T) {
	ports := &recordedPorts{}

	said, _, err := run(ports, Args{Action: actionAdd, Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ports.open, []uint16{8080}) {
		t.Errorf("got open ports %v, want [8080]", ports.open)
	}
	if !strings.Contains(said, "http://127.9.9.9:8080") {
		t.Errorf("got %q, want the address the user can open", said)
	}
	if !strings.Contains(said, "Use http://127.9.9.9:8080 for the user") ||
		!strings.Contains(said, "use localhost:8080 inside the sandbox") {
		t.Errorf("got %q, want instructions for using the two addresses", said)
	}
}

func TestHidingClosesThePortAndSaysSo(t *testing.T) {
	ports := &recordedPorts{open: []uint16{8080}}

	said, _, err := run(ports, Args{Action: actionRemove, Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports.open) != 0 {
		t.Errorf("got open ports %v, want none", ports.open)
	}
	if said != "Exposure removed." {
		t.Errorf("got %q, want the exposure to be removed", said)
	}
}

func TestListingSaysWhatIsOpenOrThatNothingIs(t *testing.T) {
	said, _, err := run(&recordedPorts{}, Args{Action: actionList})
	if err != nil {
		t.Fatal(err)
	}
	if said != "No ports are exposed." {
		t.Errorf("got %q", said)
	}

	said, _, err = run(&recordedPorts{open: []uint16{3000, 8080}}, Args{Action: actionList})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3000", "8080", "http://127.9.9.9:3000"} {
		if !strings.Contains(said, want) {
			t.Errorf("got %q, want it to name %q", said, want)
		}
	}
}

func TestARefusalFromTheHarnessReachesTheModel(t *testing.T) {
	refused := errors.New("port 8080 is already exposed")
	ports := &recordedPorts{refusal: refused}

	if _, _, err := run(ports, Args{Action: actionAdd, Port: 8080}); !errors.Is(err, refused) {
		t.Errorf("got %v, want the harness's reason", err)
	}
	if _, _, err := run(ports, Args{Action: actionRemove, Port: 8080}); !errors.Is(err, refused) {
		t.Errorf("got %v, want the harness's reason", err)
	}
}

func TestACallIsRefusedBeforeItReachesTheHarness(t *testing.T) {
	for name, args := range map[string]Args{
		"unknown action":    {Action: "expose", Port: 8080},
		"no port":           {Action: actionAdd},
		"port out of range": {Action: actionAdd, Port: 70000},
		"port with list":    {Action: actionList, Port: 8080},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(args); err == nil {
				t.Error("the call was accepted")
			}
		})
	}

	for name, args := range map[string]Args{
		"publish":   {Action: actionAdd, Port: 8080},
		"unpublish": {Action: actionRemove, Port: 8080},
		"list":      {Action: actionList},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(args); err != nil {
				t.Errorf("the call was refused: %v", err)
			}
		})
	}
}

func TestACallIsDescribedByItsPortAndWhatItDoesToIt(t *testing.T) {
	subject, qualifier := Describe(Args{Action: actionAdd, Port: 8080})
	if subject != "8080" || qualifier != "" {
		t.Errorf("got %q and %q, want the port alone", subject, qualifier)
	}

	subject, qualifier = Describe(Args{Action: actionRemove, Port: 8080})
	if subject != "8080" || qualifier != actionRemove {
		t.Errorf("got %q and %q", subject, qualifier)
	}

	subject, qualifier = Describe(Args{Action: actionList})
	if subject != "" || qualifier != actionList {
		t.Errorf("got %q and %q", subject, qualifier)
	}
}

func TestAPortOutOfRangeIsRefusedEvenWhereValidationWasSkipped(t *testing.T) {
	if _, _, err := run(&recordedPorts{}, Args{Action: actionAdd, Port: 70000}); err == nil {
		t.Error("a port beyond 65535 was exposed")
	}
}
