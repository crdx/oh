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
	"crdx.org/oh/pkg/toolbox/forward"
)

const (
	ForwardChange     agent.Kind = "port_grant_change"
	lowestPort        uint16     = 1024
	maxForwardedPorts            = 8
	LocalHost                    = "127.0.0.1"
)

type Forwarder struct {
	Forward func(port uint16) error
	Revoke  func(port uint16) error
}

func (self Forwarder) isConfigured() bool {
	return self.Forward != nil && self.Revoke != nil
}

type ForwardsRestoreFailure struct {
	Port uint16
	Err  error
}

type ForwardsRestoreResult struct {
	Failures []ForwardsRestoreFailure
}

type Route struct {
	Port    uint16
	JobName string
}

type Forwards struct {
	forwarder Forwarder
	host      string
	state     *access.State[[]Route]
	changes   chan agent.Event
	mutex     sync.Mutex
}

func NewForwards(forwarder Forwarder, host string) *Forwards {
	return &Forwards{
		forwarder: forwarder,
		host:      host,
		state:     access.New([]Route(nil), forwardsDefinition(host)),
		changes:   make(chan agent.Event, maxForwardedPorts),
	}
}

func NewRestoredForwards(
	forwarder Forwarder,
	host string,
	recordedRoutes []Route,
) (*Forwards, ForwardsRestoreResult) {
	ports := NewForwards(forwarder, host)
	result := ForwardsRestoreResult{}
	current := make([]Route, 0, len(recordedRoutes))

	for _, route := range canonicalRoutes(recordedRoutes) {
		if err := ports.open(route.Port); err != nil {
			result.Failures = append(result.Failures, ForwardsRestoreFailure{Port: route.Port, Err: err})
			continue
		}
		current = append(current, route)
	}

	ports.state = access.NewRestored(current, canonicalRoutes(recordedRoutes), forwardsDefinition(host))

	return ports, result
}

func (self *Forwards) GetCurrent() []uint16 {
	return routePorts(self.state.GetCurrent())
}

func (self *Forwards) GetRoutes() []Route {
	return self.state.GetCurrent()
}

func (self *Forwards) URL(port uint16) string {
	return URL(self.host, port)
}

func (self *Forwards) Changes() <-chan agent.Event {
	return self.changes
}

func (self *Forwards) Peek() string {
	return self.state.Peek()
}

func (self *Forwards) Inject() string {
	return self.state.Inject()
}

func (self *Forwards) Forward(port uint16) (agent.Event, error) {
	return self.forward(port, "")
}

func (self *Forwards) Revoke(port uint16) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.state.GetCurrent()
	if !slices.ContainsFunc(current, func(route Route) bool { return route.Port == port }) {
		return agent.Event{}, fmt.Errorf("port %d is not forwarded", port)
	}
	if !self.forwarder.isConfigured() {
		return agent.Event{}, errors.New("the forwarded port was lost before it could be closed")
	}
	if err := self.forwarder.Revoke(port); err != nil {
		return agent.Event{}, err
	}

	current = slices.DeleteFunc(current, func(route Route) bool { return route.Port == port })
	self.state.Replace(current)

	return ForwardChangeEvent(self.host, port, current)
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

type forwardEventState struct {
	Host       string            `json:"host,omitempty"`
	Ports      []uint16          `json:"ports"`
	JobNames   map[uint16]string `json:"job_names,omitempty"`
	IsAdoption bool              `json:"adoption,omitempty"`
}

func ForwardChangeEvent(host string, port uint16, routes []Route) (agent.Event, error) {
	return forwardChangeEvent(host, port, routes, false)
}

func forwardChangeEvent(host string, port uint16, routes []Route, isAdoption bool) (agent.Event, error) {
	routes = canonicalRoutes(routes)
	state := forwardEventState{
		Host:       host,
		Ports:      routePorts(routes),
		JobNames:   make(map[uint16]string),
		IsAdoption: isAdoption,
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

	return agent.Event{Kind: ForwardChange, Name: strconv.Itoa(int(port)), State: encodedState}, nil
}

func decodeForwardEvent(event agent.Event) ([]Route, error) {
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

func decodeState(event agent.Event) (forwardEventState, error) {
	var state forwardEventState
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

func LastRecordedForwards(events []agent.Event) ([]Route, bool) {
	return access.LastRecorded(events, ForwardChange, decodeForwardEvent)
}

func ForwardSummary(event agent.Event) (string, bool) {
	if event.Kind != ForwardChange {
		return "", false
	}
	routes, err := decodeForwardEvent(event)
	if err != nil {
		return "", false
	}
	if len(routes) == 0 {
		return "none", true
	}
	if len(routes) == 1 {
		return "1 forwarded port", true
	}

	return fmt.Sprintf("%d forwarded ports", len(routes)), true
}

func ForwardNotice(event agent.Event) (string, bool) {
	if event.Kind != ForwardChange || event.Name == "" {
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
	if jobName := state.JobNames[port]; state.IsAdoption && jobName != "" {
		return "Port " + event.Name + " now belongs to job " + jobName + ".", true
	}
	if slices.Contains(state.Ports, port) {
		return "Port " + event.Name + " is now forwarded to " + URL(state.Host, port) + ".", true
	}

	return "Port " + event.Name + " is no longer forwarded.", true
}

func ParsePort(writtenPort string) (uint16, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(writtenPort), 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("port is %q, want a number from 1 to 65535", writtenPort)
	}

	return uint16(port), nil
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

func forwardsDefinition(host string) access.Definition[[]Route] {
	return access.Definition[[]Route]{
		Clone: canonicalRoutes,
		Describe: func(knownRoutes []Route, currentRoutes []Route) string {
			return describeForwardChanges(host, knownRoutes, currentRoutes)
		},
	}
}

func describeForwardChanges(host string, knownRoutes []Route, currentRoutes []Route) string {
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

type ForwardsModelAccess struct {
	ports *Forwards
}

func (self *Forwards) ForModel() ForwardsModelAccess {
	return ForwardsModelAccess{ports: self}
}

func (self *Forwards) forward(port uint16, jobName string) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.state.GetCurrent()
	if index := slices.IndexFunc(current, func(route Route) bool { return route.Port == port }); index >= 0 {
		if jobName == "" || current[index].JobName != "" {
			return agent.Event{}, fmt.Errorf("port %d is already forwarded", port)
		}
		current[index].JobName = jobName
		self.state.Replace(current)

		return forwardChangeEvent(self.host, port, current, true)
	}
	if err := self.open(port); err != nil {
		return agent.Event{}, err
	}

	current = canonicalRoutes(append(current, Route{Port: port, JobName: jobName}))
	self.state.Replace(current)

	return ForwardChangeEvent(self.host, port, current)
}

func (self ForwardsModelAccess) Forward(port uint16, jobName string) (string, error) {
	if self.ports.isRouted(Route{Port: port, JobName: jobName}) {
		return self.ports.URL(port), nil
	}
	event, err := self.ports.forward(port, jobName)
	if err != nil {
		return "", err
	}
	self.ports.announce(event)

	return self.ports.URL(port), nil
}

func (self ForwardsModelAccess) Revoke(port uint16) error {
	event, err := self.ports.Revoke(port)
	if err != nil {
		return err
	}
	self.ports.announce(event)

	return nil
}

func (self ForwardsModelAccess) List() []forward.Publication {
	routes := self.ports.GetRoutes()
	publications := make([]forward.Publication, 0, len(routes))
	for _, route := range routes {
		publications = append(publications, forward.Publication{
			Port:    route.Port,
			JobName: route.JobName,
			URL:     self.ports.URL(route.Port),
		})
	}

	return publications
}

func (self *Forwards) isRouted(route Route) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return slices.Contains(self.state.GetCurrent(), route)
}

func (self *Forwards) open(port uint16) error {
	if err := self.judge(port); err != nil {
		return err
	}

	return self.forwarder.Forward(port)
}

func (self *Forwards) judge(port uint16) error {
	if !self.forwarder.isConfigured() {
		return errors.New(
			"a forwarded sandbox port needs the sandbox's own network, which this session does not have",
		)
	}
	if port == 0 {
		return errors.New("port 0 is invalid")
	}
	if port < lowestPort {
		return fmt.Errorf("port %d is below %d, which an unprivileged harness cannot bind", port, lowestPort)
	}
	if len(self.state.GetCurrent()) >= maxForwardedPorts {
		return fmt.Errorf("%d ports are forwarded already, which is as many as a session may open", maxForwardedPorts)
	}

	return nil
}

func (self *Forwards) announce(event agent.Event) {
	select {
	case self.changes <- event:
	default:
	}
}
