package forward

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/tool"
)

type recordedPorts struct {
	open    []Publication
	refusal error
}

func (self *recordedPorts) Forward(port uint16, taskName string) (string, error) {
	if self.refusal != nil {
		return "", self.refusal
	}
	self.open = append(self.open, Publication{Port: port, TaskName: taskName})

	return "http://127.9.9.9:" + strconv.Itoa(int(port)), nil
}

func (self *recordedPorts) Revoke(port uint16) error {
	if self.refusal != nil {
		return self.refusal
	}
	self.open = slices.DeleteFunc(self.open, func(publication Publication) bool { return publication.Port == port })

	return nil
}

func (self *recordedPorts) List() []Publication {
	publications := slices.Clone(self.open)
	for i := range publications {
		publications[i].URL = "http://127.9.9.9:" + strconv.Itoa(int(publications[i].Port))
	}

	return publications
}

func TestACancelledPortRequestChangesNothing(t *testing.T) {
	ports := &recordedPorts{}
	call, err := New(ports).Parse(`{"action":"add","port":8080}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop.Because("the user pressed escape"))

	_, err = call.Exec(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want a cancellation", err)
	}
	if want := "the port request stopped because the user pressed escape"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
	if len(ports.open) != 0 {
		t.Errorf("forwarded ports after cancellation: %v", ports.open)
	}
}

func TestForwardingSaysWhereTheUserCanOpenIt(t *testing.T) {
	ports := &recordedPorts{}

	said, err := run(ports, Args{Action: actionAdd, Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports.open) != 1 || ports.open[0].Port != 8080 || ports.open[0].TaskName != "" {
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

func TestForwardingCanAssociateThePortWithATask(t *testing.T) {
	ports := &recordedPorts{}

	if _, err := run(ports, Args{Action: actionAdd, Port: 8080, TaskName: "docs"}); err != nil {
		t.Fatal(err)
	}
	if len(ports.open) != 1 || ports.open[0] != (Publication{Port: 8080, TaskName: "docs"}) {
		t.Errorf("got open ports %#v, want docs associated with 8080", ports.open)
	}
}

func TestRevokingClosesThePortAndSaysSo(t *testing.T) {
	ports := &recordedPorts{open: []Publication{{Port: 8080}}}

	said, err := run(ports, Args{Action: actionRemove, Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports.open) != 0 {
		t.Errorf("got open ports %v, want none", ports.open)
	}
	if said != "Forward removed." {
		t.Errorf("got %q, want the forward to be removed", said)
	}
}

func TestListingSaysWhatIsOpenOrThatNothingIs(t *testing.T) {
	said, err := run(&recordedPorts{}, Args{Action: actionList})
	if err != nil {
		t.Fatal(err)
	}
	if said != "No ports are forwarded." {
		t.Errorf("got %q", said)
	}

	said, err = run(&recordedPorts{open: []Publication{{Port: 3000}, {Port: 8080, TaskName: "docs"}}}, Args{Action: actionList})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3000", "docs:8080", "http://127.9.9.9:3000"} {
		if !strings.Contains(said, want) {
			t.Errorf("got %q, want it to name %q", said, want)
		}
	}
}

func TestARefusalFromTheHarnessReachesTheModel(t *testing.T) {
	refused := errors.New("port 8080 is already forwarded")
	ports := &recordedPorts{refusal: refused}

	if _, err := run(ports, Args{Action: actionAdd, Port: 8080}); !errors.Is(err, refused) {
		t.Errorf("got %v, want the harness's reason", err)
	}
	if _, err := run(ports, Args{Action: actionRemove, Port: 8080}); !errors.Is(err, refused) {
		t.Errorf("got %v, want the harness's reason", err)
	}
}

func TestACallIsRefusedBeforeItReachesTheHarness(t *testing.T) {
	for name, args := range map[string]Args{
		"unknown action":    {Action: "forward", Port: 8080},
		"no port":           {Action: actionAdd},
		"port out of range": {Action: actionAdd, Port: 70000},
		"port with list":    {Action: actionList, Port: 8080},
		"task with list":    {Action: actionList, TaskName: "docs"},
		"task with remove":  {Action: actionRemove, Port: 8080, TaskName: "docs"},
		"invalid task name": {Action: actionAdd, Port: 8080, TaskName: "Docs"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(args); err == nil {
				t.Error("the call was accepted")
			}
		})
	}

	for name, args := range map[string]Args{
		"publish":                      {Action: actionAdd, Port: 8080},
		"publish with task":            {Action: actionAdd, Port: 8080, TaskName: "docs"},
		"publish with a numbered task": {Action: actionAdd, Port: 8080, TaskName: "abcdefghij-1"},
		"unpublish":                    {Action: actionRemove, Port: 8080},
		"list":                         {Action: actionList},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(args); err != nil {
				t.Errorf("the call was refused: %v", err)
			}
		})
	}
}

func TestACallIsRenderedByItsAction(t *testing.T) {
	for name, shape := range map[string]struct {
		args Args
		want tool.CallRendering
	}{
		"forward": {
			args: Args{Action: actionAdd, Port: 8080},
			want: tool.CallRendering{Kind: "forward_add", ReportsStatus: true, Subject: "8080"},
		},
		"associated forward": {
			args: Args{Action: actionAdd, Port: 8080, TaskName: "docs"},
			want: tool.CallRendering{Kind: "forward_add", ReportsStatus: true, Subject: "docs:8080"},
		},
		"unforward": {
			args: Args{Action: actionRemove, Port: 8080},
			want: tool.CallRendering{Kind: "forward_remove", ReportsStatus: true, Subject: "8080"},
		},
		"list": {
			args: Args{Action: actionList},
			want: tool.CallRendering{Kind: "forward_list", Subject: "forwards"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if rendering := Describe(shape.args); !reflect.DeepEqual(rendering, shape.want) {
				t.Errorf("got %#v, want %#v", rendering, shape.want)
			}
		})
	}
}

func TestAPortOutOfRangeIsRefusedEvenWhereValidationWasSkipped(t *testing.T) {
	if _, err := run(&recordedPorts{}, Args{Action: actionAdd, Port: 70000}); err == nil {
		t.Error("a port beyond 65535 was forwarded")
	}
}
