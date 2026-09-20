package portgrant

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/pkg/agent"
)

const (
	SandboxToHostChange   agent.Kind = "sandbox_to_host_change"
	maxSandboxToHostPorts int        = 8
)

type SandboxToHostExposer struct {
	Expose func(port uint16) error
	Hide   func(port uint16) error
}

func (self SandboxToHostExposer) isConfigured() bool {
	return self.Expose != nil && self.Hide != nil
}

type SandboxToHostRestoreFailure struct {
	Port uint16
	Err  error
}

type SandboxToHostRestoreResult struct {
	Failures []SandboxToHostRestoreFailure
}

type SandboxToHost struct {
	exposer       SandboxToHostExposer
	reservedPorts func() []uint16
	state         *access.State[[]uint16]
	mutex         sync.Mutex
}

func NewSandboxToHost(exposer SandboxToHostExposer, reservedPorts func() []uint16) *SandboxToHost {
	return &SandboxToHost{
		exposer:       exposer,
		reservedPorts: reservedPorts,
		state:         access.New([]uint16(nil), sandboxToHostDefinition()),
	}
}

func NewRestoredSandboxToHost(
	exposer SandboxToHostExposer,
	reservedPorts func() []uint16,
	recordedPorts []uint16,
) (*SandboxToHost, SandboxToHostRestoreResult) {
	grants := NewSandboxToHost(exposer, reservedPorts)
	result := SandboxToHostRestoreResult{}
	current := make([]uint16, 0, len(recordedPorts))

	for _, port := range canonicalPorts(recordedPorts) {
		if err := grants.open(port); err != nil {
			result.Failures = append(result.Failures, SandboxToHostRestoreFailure{Port: port, Err: err})
			continue
		}
		current = append(current, port)
	}

	grants.state = access.NewRestored(current, canonicalPorts(recordedPorts), sandboxToHostDefinition())
	return grants, result
}

func (self *SandboxToHost) GetCurrent() []uint16 {
	return self.state.GetCurrent()
}

func (self *SandboxToHost) Peek() string {
	return self.state.Peek()
}

func (self *SandboxToHost) Inject() string {
	return self.state.Inject()
}

func (self *SandboxToHost) Expose(port uint16) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.state.GetCurrent()
	if slices.Contains(current, port) {
		return agent.Event{}, fmt.Errorf("host loopback port %d is already exposed", port)
	}
	if err := self.open(port); err != nil {
		return agent.Event{}, err
	}

	current = canonicalPorts(append(current, port))
	self.state.Replace(current)
	return SandboxToHostChangeEvent(port, current)
}

func (self *SandboxToHost) Revoke(port uint16) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.state.GetCurrent()
	if !slices.Contains(current, port) {
		return agent.Event{}, fmt.Errorf("host loopback port %d is not exposed", port)
	}
	if !self.exposer.isConfigured() {
		return agent.Event{}, errors.New("the host loopback port was lost before it could be revoked")
	}
	if err := self.exposer.Hide(port); err != nil {
		return agent.Event{}, err
	}

	current = slices.DeleteFunc(current, func(openPort uint16) bool { return openPort == port })
	self.state.Replace(current)
	return SandboxToHostChangeEvent(port, current)
}

func (self *SandboxToHost) open(port uint16) error {
	if !self.exposer.isConfigured() {
		return errors.New("a host loopback port needs the sandbox's own network, which this session does not have")
	}
	if port == 0 {
		return errors.New("port 0 is invalid")
	}
	if self.reservedPorts != nil && slices.Contains(self.reservedPorts(), port) {
		return fmt.Errorf("port %d is already exposed from the sandbox", port)
	}
	if len(self.state.GetCurrent()) >= maxSandboxToHostPorts {
		return fmt.Errorf("%d host loopback ports are exposed already, which is as many as a session may open", maxSandboxToHostPorts)
	}

	return self.exposer.Expose(port)
}

type sandboxToHostEventState struct {
	Ports []uint16 `json:"ports"`
}

func SandboxToHostChangeEvent(port uint16, ports []uint16) (agent.Event, error) {
	state, err := json.Marshal(sandboxToHostEventState{Ports: canonicalPorts(ports)})
	if err != nil {
		return agent.Event{}, err
	}
	return agent.Event{Kind: SandboxToHostChange, Name: strconv.Itoa(int(port)), State: state}, nil
}

func decodeSandboxToHostEvent(event agent.Event) ([]uint16, error) {
	var state sandboxToHostEventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil, err
	}
	if slices.Contains(state.Ports, 0) {
		return nil, errors.New("invalid host port grant state")
	}
	return canonicalPorts(state.Ports), nil
}

func LastRecordedSandboxToHost(events []agent.Event) ([]uint16, bool) {
	return access.LastRecorded(events, SandboxToHostChange, decodeSandboxToHostEvent)
}

func SandboxToHostSummary(event agent.Event) (string, bool) {
	if event.Kind != SandboxToHostChange {
		return "", false
	}
	ports, err := decodeSandboxToHostEvent(event)
	if err != nil {
		return "", false
	}
	if len(ports) == 0 {
		return "none", true
	}
	if len(ports) == 1 {
		return "1 host port", true
	}
	return fmt.Sprintf("%d host ports", len(ports)), true
}

func SandboxToHostNotice(event agent.Event) (string, bool) {
	if event.Kind != SandboxToHostChange || event.Name == "" {
		return "", false
	}
	portNumber, err := strconv.ParseUint(event.Name, 10, 16)
	if err != nil || portNumber == 0 {
		return "", false
	}
	ports, err := decodeSandboxToHostEvent(event)
	if err != nil {
		return "", false
	}
	port := uint16(portNumber)
	if slices.Contains(ports, port) {
		return "Host loopback port " + event.Name + " exposed to sandbox.", true
	}
	return "Host loopback port " + event.Name + " no longer exposed to sandbox.", true
}

func sandboxToHostDefinition() access.Definition[[]uint16] {
	return access.Definition[[]uint16]{
		Clone: canonicalPorts,
		Describe: func(knownPorts []uint16, currentPorts []uint16) string {
			var clauses []string
			for _, port := range currentPorts {
				if !slices.Contains(knownPorts, port) {
					clauses = append(clauses, "TCP port "+strconv.Itoa(int(port))+" on the host loopback is now reachable at 127.0.0.1:"+strconv.Itoa(int(port))+" from the sandbox.")
				}
			}
			for _, port := range knownPorts {
				if !slices.Contains(currentPorts, port) {
					clauses = append(clauses, "TCP port "+strconv.Itoa(int(port))+" on the host loopback is no longer reachable from the sandbox.")
				}
			}
			return strings.Join(clauses, " ")
		},
	}
}
