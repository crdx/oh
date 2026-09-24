package portgrant

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/toolbox/expose"
)

const (
	HostToSandboxChange agent.Kind = "port_grant_change"
	lowestPort          uint16     = 1024
	maxExposedPorts                = 8
	LocalHost                      = "127.0.0.1"
)

type HostToSandboxExposer struct {
	Expose func(port uint16) error
	Hide   func(port uint16) error
}

func (self HostToSandboxExposer) isConfigured() bool {
	return self.Expose != nil && self.Hide != nil
}

type HostToSandboxRestoreFailure struct {
	Port uint16
	Err  error
}

type HostToSandboxRestoreResult struct {
	Failures []HostToSandboxRestoreFailure
}

type Route struct {
	Port    uint16
	JobName string
}

type HostToSandbox struct {
	exposer               HostToSandboxExposer
	host                  string
	getSandboxToHostPorts func() []uint16
	state                 *access.State[[]Route]
	changes               chan agent.Event
	mutex                 sync.Mutex
}

func NewHostToSandbox(exposer HostToSandboxExposer, host string) *HostToSandbox {
	return &HostToSandbox{
		exposer: exposer,
		host:    host,
		state:   access.New([]Route(nil), hostToSandboxDefinition(host)),
		changes: make(chan agent.Event, maxExposedPorts),
	}
}

func NewRestoredHostToSandbox(
	exposer HostToSandboxExposer,
	host string,
	recordedRoutes []Route,
) (*HostToSandbox, HostToSandboxRestoreResult) {
	ports := NewHostToSandbox(exposer, host)
	result := HostToSandboxRestoreResult{}
	current := make([]Route, 0, len(recordedRoutes))

	for _, route := range canonicalRoutes(recordedRoutes) {
		if err := ports.open(route.Port); err != nil {
			result.Failures = append(result.Failures, HostToSandboxRestoreFailure{Port: route.Port, Err: err})
			continue
		}
		current = append(current, route)
	}

	ports.state = access.NewRestored(current, canonicalRoutes(recordedRoutes), hostToSandboxDefinition(host))

	return ports, result
}

func (self *HostToSandbox) GetCurrent() []uint16 {
	return routePorts(self.state.GetCurrent())
}

func (self *HostToSandbox) GetRoutes() []Route {
	return self.state.GetCurrent()
}

func (self *HostToSandbox) URL(port uint16) string {
	return URL(self.host, port)
}

func (self *HostToSandbox) Changes() <-chan agent.Event {
	return self.changes
}

func (self *HostToSandbox) SetSandboxToHostPorts(getSandboxToHostPorts func() []uint16) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.getSandboxToHostPorts = getSandboxToHostPorts
}

func (self *HostToSandbox) Peek() string {
	return self.state.Peek()
}

func (self *HostToSandbox) Inject() string {
	return self.state.Inject()
}

func (self *HostToSandbox) Expose(port uint16) (agent.Event, error) {
	return self.expose(port, "")
}

func (self *HostToSandbox) Hide(port uint16) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.state.GetCurrent()
	if !slices.ContainsFunc(current, func(route Route) bool { return route.Port == port }) {
		return agent.Event{}, fmt.Errorf("port %d is not exposed", port)
	}
	if !self.exposer.isConfigured() {
		return agent.Event{}, errors.New("the exposed port was lost before it could be closed")
	}
	if err := self.exposer.Hide(port); err != nil {
		return agent.Event{}, err
	}

	current = slices.DeleteFunc(current, func(route Route) bool { return route.Port == port })
	self.state.Replace(current)

	return HostToSandboxChangeEvent(self.host, port, current)
}

func URL(host string, port uint16) string {
	return "http://" + net.JoinHostPort(host, strconv.Itoa(int(port)))
}

func AddressFor(sessionName string) string {
	digest := sha256.Sum256([]byte(sessionName))
	address := netip.AddrFrom4([4]byte{127, digest[0], digest[1], digest[2]})
	if address.As4()[3] == 0 || address.As4()[3] == 255 || address.String() == LocalHost {
		return netip.AddrFrom4([4]byte{127, digest[0], digest[1], 1 + digest[2]%254}).String()
	}

	return address.String()
}

type hostToSandboxEventState struct {
	Host     string            `json:"host,omitempty"`
	Ports    []uint16          `json:"ports"`
	JobNames map[uint16]string `json:"job_names,omitempty"`
}

func HostToSandboxChangeEvent(host string, port uint16, routes []Route) (agent.Event, error) {
	routes = canonicalRoutes(routes)
	state := hostToSandboxEventState{
		Host:     host,
		Ports:    routePorts(routes),
		JobNames: make(map[uint16]string),
	}
	for _, route := range routes {
		if route.JobName != "" {
			state.JobNames[route.Port] = route.JobName
		}
	}

	encodedState, err := json.Marshal(state)
	if err != nil {
		return agent.Event{}, err
	}

	return agent.Event{Kind: HostToSandboxChange, Name: strconv.Itoa(int(port)), State: encodedState}, nil
}

func decodeHostToSandboxEvent(event agent.Event) ([]Route, error) {
	state, err := decodeState(event)
	if err != nil {
		return nil, err
	}

	routes := make([]Route, 0, len(state.Ports))
	for _, port := range state.Ports {
		routes = append(routes, Route{Port: port, JobName: state.JobNames[port]})
	}

	return canonicalRoutes(routes), nil
}

func decodeState(event agent.Event) (hostToSandboxEventState, error) {
	var state hostToSandboxEventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return state, err
	}
	if slices.Contains(state.Ports, 0) {
		return state, errors.New("invalid port grant state")
	}
	if state.Host == "" {
		state.Host = LocalHost
	}

	return state, nil
}

func LastRecordedHostToSandbox(events []agent.Event) ([]Route, bool) {
	return access.LastRecorded(events, HostToSandboxChange, decodeHostToSandboxEvent)
}

func HostToSandboxSummary(event agent.Event) (string, bool) {
	if event.Kind != HostToSandboxChange {
		return "", false
	}
	routes, err := decodeHostToSandboxEvent(event)
	if err != nil {
		return "", false
	}
	if len(routes) == 0 {
		return "none", true
	}
	if len(routes) == 1 {
		return "1 sandbox port", true
	}

	return fmt.Sprintf("%d sandbox ports", len(routes)), true
}

func HostToSandboxNotice(event agent.Event) (string, bool) {
	if event.Kind != HostToSandboxChange || event.Name == "" {
		return "", false
	}
	port, err := ParsePort(event.Name)
	if err != nil {
		return "", false
	}
	state, err := decodeState(event)
	if err != nil {
		return "", false
	}
	if slices.Contains(state.Ports, port) {
		return "Sandbox port " + event.Name + " exposed at " + URL(state.Host, port) + ".", true
	}

	return "Sandbox port " + event.Name + " no longer exposed to host.", true
}

func ParsePort(writtenPort string) (uint16, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(writtenPort), 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("port is %q, want a number from 1 to 65535", writtenPort)
	}

	return uint16(port), nil
}

func canonicalPorts(ports []uint16) []uint16 {
	sortedPorts := slices.Clone(ports)
	slices.Sort(sortedPorts)

	return slices.Compact(sortedPorts)
}

func canonicalRoutes(routes []Route) []Route {
	sortedRoutes := slices.Clone(routes)
	slices.SortFunc(sortedRoutes, func(left Route, right Route) int {
		return int(left.Port) - int(right.Port)
	})

	return slices.CompactFunc(sortedRoutes, func(left Route, right Route) bool {
		return left.Port == right.Port
	})
}

func routePorts(routes []Route) []uint16 {
	ports := make([]uint16, 0, len(routes))
	for _, route := range canonicalRoutes(routes) {
		ports = append(ports, route.Port)
	}

	return ports
}

func hostToSandboxDefinition(host string) access.Definition[[]Route] {
	return access.Definition[[]Route]{
		Clone: canonicalRoutes,
		Describe: func(knownRoutes []Route, currentRoutes []Route) string {
			return describeHostToSandboxChanges(host, knownRoutes, currentRoutes)
		},
	}
}

func describeHostToSandboxChanges(host string, knownRoutes []Route, currentRoutes []Route) string {
	var clauses []string
	for _, route := range currentRoutes {
		if slices.ContainsFunc(knownRoutes, func(knownRoute Route) bool { return knownRoute.Port == route.Port }) {
			continue
		}
		clauses = append(clauses,
			"The user can now reach TCP port "+strconv.Itoa(int(route.Port))+
				" on this sandbox's loopback, at "+URL(host, route.Port)+" on their own machine.",
		)
	}
	for _, route := range knownRoutes {
		if slices.ContainsFunc(currentRoutes, func(current Route) bool { return current.Port == route.Port }) {
			continue
		}
		clauses = append(clauses,
			"TCP port "+strconv.Itoa(int(route.Port))+" is no longer reachable from outside the sandbox.",
		)
	}

	return strings.Join(clauses, " ")
}

type HostToSandboxModelAccess struct {
	ports *HostToSandbox
}

func (self *HostToSandbox) ForModel() HostToSandboxModelAccess {
	return HostToSandboxModelAccess{ports: self}
}

func (self *HostToSandbox) expose(port uint16, jobName string) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.state.GetCurrent()
	if slices.ContainsFunc(current, func(route Route) bool { return route.Port == port }) {
		return agent.Event{}, fmt.Errorf("port %d is already exposed", port)
	}
	if err := self.open(port); err != nil {
		return agent.Event{}, err
	}

	current = canonicalRoutes(append(current, Route{Port: port, JobName: jobName}))
	self.state.Replace(current)

	return HostToSandboxChangeEvent(self.host, port, current)
}

func (self HostToSandboxModelAccess) Expose(port uint16, jobName string) (string, error) {
	event, err := self.ports.expose(port, jobName)
	if err != nil {
		return "", err
	}
	self.ports.announce(event)

	return self.ports.URL(port), nil
}

func (self HostToSandboxModelAccess) Hide(port uint16) error {
	event, err := self.ports.Hide(port)
	if err != nil {
		return err
	}
	self.ports.announce(event)

	return nil
}

func (self HostToSandboxModelAccess) List() []expose.Publication {
	routes := self.ports.GetRoutes()
	publications := make([]expose.Publication, 0, len(routes))
	for _, route := range routes {
		publications = append(publications, expose.Publication{
			Port:    route.Port,
			JobName: route.JobName,
			URL:     self.ports.URL(route.Port),
		})
	}

	return publications
}

func (self *HostToSandbox) open(port uint16) error {
	if err := self.judge(port); err != nil {
		return err
	}

	return self.exposer.Expose(port)
}

func (self *HostToSandbox) judge(port uint16) error {
	if !self.exposer.isConfigured() {
		return errors.New(
			"a sandbox port exposed to the host needs the sandbox's own network, which this session does not have",
		)
	}
	if port == 0 {
		return errors.New("port 0 is invalid")
	}
	if port < lowestPort {
		return fmt.Errorf("port %d is below %d, which an unprivileged harness cannot bind", port, lowestPort)
	}
	if self.getSandboxToHostPorts != nil && slices.Contains(self.getSandboxToHostPorts(), port) {
		return fmt.Errorf("port %d already forwards to the host, so nothing inside is listening on it", port)
	}
	if len(self.state.GetCurrent()) >= maxExposedPorts {
		return fmt.Errorf("%d ports are exposed already, which is as many as a session may open", maxExposedPorts)
	}

	return nil
}

func (self *HostToSandbox) announce(event agent.Event) {
	select {
	case self.changes <- event:
	default:
	}
}
