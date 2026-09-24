package demo

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
)

const (
	simulatedModel     = "simulation"
	typingPace         = 35 * time.Millisecond
	thinkingDelay      = 400 * time.Millisecond
	loopbackAddress    = "127.0.0.1:0"
	stateDirPrefix     = "demo-"
	lockName           = ".lock"
	headerTimeout      = 30 * time.Second
	shutdownLimit      = time.Second
	demonstratedEffort = "high"
)

type Session struct {
	EndpointURL string
	Selection   string

	server               *http.Server
	stateDir             string
	lock                 *os.File
	inheritedStateDir    string
	hasInheritedStateDir bool
}

func Start() (*Session, error) {
	return start(scenario())
}

func start(script *sim.Scenario) (*Session, error) {
	inheritedStateDir, hasInheritedStateDir := os.LookupEnv(location.StateDirVariable)

	stateDir, lock, err := openStateDir()
	if err != nil {
		return nil, err
	}

	self := &Session{
		Selection: model.Selection{
			Provider: model.AnthropicProvider,
			Model:    simulatedModel,
			Effort:   demonstratedEffort,
		}.String(),
		stateDir:             stateDir,
		lock:                 lock,
		inheritedStateDir:    inheritedStateDir,
		hasInheritedStateDir: hasInheritedStateDir,
	}

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(context.Background(), "tcp", loopbackAddress)
	if err != nil {
		self.forgetState()
		return nil, err
	}

	endpoint := sim.NewResponder(script, answer)
	server := &http.Server{Handler: endpoint, ReadHeaderTimeout: headerTimeout}

	self.server = server
	self.EndpointURL = endpoint.Addresses("http://" + listener.Addr().String())[sim.Messages]

	go func() { _ = server.Serve(listener) }()

	if err := os.Setenv(location.StateDirVariable, stateDir); err != nil {
		self.Close()
		return nil, err
	}

	if err := self.rememberOfferedModel(); err != nil {
		self.Close()
		return nil, err
	}

	return self, nil
}

func (self *Session) Close() {
	if self.server != nil {
		ctx, stopWaiting := context.WithTimeout(context.Background(), shutdownLimit)
		defer stopWaiting()

		_ = self.server.Shutdown(ctx)
		self.server = nil
	}

	self.forgetState()
}

func (self *Session) rememberOfferedModel() error {
	return model.StoreSimulated(
		location.GetModelCachePath(),
		model.AnthropicProvider,
		[]agent.Model{sim.OfferedModel(simulatedModel)},
	)
}

func (self *Session) forgetState() {
	if self.hasInheritedStateDir {
		_ = os.Setenv(location.StateDirVariable, self.inheritedStateDir)
	} else {
		_ = os.Unsetenv(location.StateDirVariable)
	}

	if self.lock != nil {
		_ = self.lock.Close()
		self.lock = nil
	}

	if self.stateDir != "" {
		_ = os.RemoveAll(self.stateDir)
		self.stateDir = ""
	}
}

func scenario() *sim.Scenario {
	return &sim.Scenario{
		Model: simulatedModel,
		Pace:  sim.Duration{Duration: typingPace},
		Delay: sim.Duration{Duration: thinkingDelay},
	}
}

func openStateDir() (string, *os.File, error) {
	root := location.GetStateDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", nil, err
	}

	sweepAbandonedStateDirs(root)

	stateDir, err := os.MkdirTemp(root, stateDirPrefix)
	if err != nil {
		return "", nil, err
	}

	lock, err := claimStateDir(stateDir)
	if err != nil {
		_ = os.RemoveAll(stateDir)
		return "", nil, err
	}

	return stateDir, lock, nil
}

func claimStateDir(stateDir string) (*os.File, error) {
	//nolint:gosec // the directory is the one this session made
	lock, err := os.OpenFile(filepath.Join(stateDir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, err
	}

	return lock, nil
}

func sweepAbandonedStateDirs(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), stateDirPrefix) {
			continue
		}

		leftover := filepath.Join(root, entry.Name())

		lock, err := claimStateDir(leftover)
		if err != nil {
			continue
		}

		_ = os.RemoveAll(leftover)
		_ = lock.Close()
	}
}
