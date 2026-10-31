package scene

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/background"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/vt"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/sandbox"
	"crdx.org/oh/pkg/session"
)

const (
	SessionName = "tame-impala"

	Variable = "OH_SCENE"

	provider      = "opencode-go"
	defaultModel  = "fake"
	historyLimit  = 1000
	defaultSettle = 400 * time.Millisecond
	startDeadline = 20 * time.Second
	keyDeadline   = 10 * time.Second
	exitDeadline  = 5 * time.Second

	darkBackground   = "\x1b]11;rgb:1d1d/1f1f/2121\x1b\\"
	deviceAttributes = "\x1b[?62;4c"
)

type Scene struct {
	Journal string `toml:"journal"`

	History []string `toml:"history"`

	Keys []string `toml:"keys"`

	Replies []string `toml:"replies"`

	Model string `toml:"model"`

	Config string `toml:"config"`

	Settle sim.Duration `toml:"settle"`

	Yolo bool `toml:"yolo"`
}

func Render(ctx context.Context, path string, columns int, rows int) (string, error) {
	var scene Scene
	metadata, err := toml.DecodeFile(path, &scene)
	if err != nil {
		return "", err
	}
	if unknownKeys := metadata.Undecoded(); len(unknownKeys) > 0 {
		return "", fmt.Errorf("%s: unknown setting %q", path, unknownKeys[0].String())
	}

	keys, err := encodeKeys(scene.Keys)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}

	var events []agent.Event
	if scene.Journal != "" {
		events, err = readEvents(filepath.Join(filepath.Dir(path), scene.Journal))
		if err != nil {
			return "", err
		}
	}

	if scene.Model == "" {
		scene.Model = defaultModel
	}

	settle := scene.Settle.Duration
	if settle <= 0 {
		settle = defaultSettle
	}

	return run(ctx, scene, events, keys, columns, rows, settle)
}

func run(ctx context.Context, scene Scene, events []agent.Event, keys []string, columns int, rows int, settle time.Duration) (string, error) {
	parent, err := scratchParent()
	if err != nil {
		return "", err
	}
	root, err := os.MkdirTemp(parent, "oh-scene-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(root) }()

	directories, err := layOut(root, scene, events)
	if err != nil {
		return "", err
	}

	endpoint, stop, err := serveSimulator(ctx, scene.Model, scene.Replies)
	if err != nil {
		return "", err
	}
	defer stop()

	binary, err := os.Executable()
	if err != nil {
		return "", err
	}

	controller, terminal, err := openTerminal(columns, rows)
	if err != nil {
		return "", err
	}
	defer func() { _ = controller.Close() }()

	command := exec.CommandContext(ctx, binary, "-r", SessionName) //nolint:gosec // oh runs itself
	command.Dir = directories.workspace
	command.Env = environment(directories, endpoint)
	command.Stdin, command.Stdout, command.Stderr = terminal, terminal, terminal
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := command.Start(); err != nil {
		_ = terminal.Close()
		return "", err
	}
	_ = terminal.Close()
	exit := make(chan error, 1)
	go func() { exit <- command.Wait() }()
	defer func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-exit:
		case <-time.After(exitDeadline):
			_ = command.Process.Kill()
			<-exit
		}
	}()

	transcript := record(controller)
	transcript.answer(background.Query, darkBackground+deviceAttributes)

	if !transcript.settle(settle, startDeadline) {
		return "", fmt.Errorf("oh did not settle within %s of starting:\n%s", startDeadline, transcript.tail())
	}
	for at, key := range keys {
		if _, err := controller.WriteString(key); err != nil {
			return "", err
		}
		if !transcript.settle(settle, keyDeadline) {
			return "", fmt.Errorf("oh did not settle within %s of key %d", keyDeadline, at+1)
		}
	}

	screen := vt.New(columns, rows)
	if err := screen.Play(transcript.String()); err != nil {
		return "", fmt.Errorf("could not settle the drawing: %w", err)
	}

	return strings.Join(screen.StyledOnScreen(), "\n") + "\n", nil
}

func scratchParent() (string, error) {
	if _, isCovered := pathutil.RelativeTo(sandbox.TmpDir, os.TempDir()); !isCovered {
		return os.TempDir(), nil
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no directory outside %s to run a scene in: %w", sandbox.TmpDir, err)
	}
	if _, isCovered := pathutil.RelativeTo(sandbox.TmpDir, cache); isCovered {
		return "", fmt.Errorf("no directory outside %s to run a scene in: the cache is at %s", sandbox.TmpDir, cache)
	}

	parent := filepath.Join(cache, "oh-scene")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}

	return parent, nil
}

type directories struct {
	home      string
	config    string
	state     string
	workspace string
}

func layOut(root string, scene Scene, events []agent.Event) (directories, error) {
	laid := directories{
		home:      filepath.Join(root, "home"),
		config:    filepath.Join(root, "config"),
		state:     filepath.Join(root, "state"),
		workspace: filepath.Join(root, "workspace"),
	}
	for _, directory := range []string{laid.home, laid.config, laid.state, laid.workspace} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return laid, err
		}
	}

	configFile := filepath.Join(laid.config, "org.crdx", "oh", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configFile), 0o700); err != nil {
		return laid, err
	}
	settings := fmt.Sprintf("version = %d\n", config.Format)
	if scene.Config != "" {
		settings += "\n" + scene.Config + "\n"
	}
	if err := os.WriteFile(configFile, []byte(settings), 0o600); err != nil {
		return laid, err
	}

	ohState := filepath.Join(laid.state, "org.crdx", "oh")
	if len(scene.History) > 0 {
		history := edit.NewHistory(filepath.Join(ohState, "data", "history"), historyLimit)
		if err := os.MkdirAll(filepath.Join(ohState, "data"), 0o700); err != nil {
			return laid, err
		}
		for _, line := range scene.History {
			history.Add(line)
		}
	}

	return laid, writeSession(filepath.Join(ohState, "sessions"), laid.workspace, scene, events)
}

func writeSession(sessionsDirectory string, workspace string, scene Scene, events []agent.Event) error {
	if err := os.MkdirAll(sessionsDirectory, 0o700); err != nil {
		return err
	}

	writer, err := store.CreateNamed(sessionsDirectory, SessionName, store.Meta{
		Model:          scene.Model,
		Provider:       provider,
		WorkspaceDir:   workspace,
		Yolo:           scene.Yolo,
		ShouldSkipWire: true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = writer.Close() }()

	if len(events) == 0 {
		events = []agent.Event{{Kind: agent.StartupEvent, State: json.RawMessage(`{"session":"` + SessionName + `"}`)}}
	}
	for _, event := range events {
		if err := writer.Event(event); err != nil {
			return err
		}
	}
	if err := writer.EnsurePersisted(); err != nil {
		return err
	}

	return writer.CompleteTurn(session.TurnSummary{})
}

func environment(laid directories, endpoint string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"TERM=xterm-256color",
		"HOME=" + laid.home,
		"XDG_CONFIG_HOME=" + laid.config,
		"XDG_STATE_HOME=" + laid.state,
		backend.EndpointVariable + "=" + endpoint,
		Variable + "=1",
	}
}

func serveSimulator(ctx context.Context, model string, replies []string) (string, func(), error) {
	turns := make([]sim.Turn, 0, len(replies))
	for _, reply := range replies {
		turns = append(turns, sim.Turn{Say: reply})
	}
	endpoint := sim.New(&sim.Scenario{Model: model, Turns: turns})

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: endpoint, ReadHeaderTimeout: keyDeadline}
	go func() { _ = server.Serve(listener) }()

	address := "http://" + listener.Addr().String()

	return endpoint.Addresses(address)[sim.Completions], func() { _ = server.Close() }, nil
}

func readEvents(path string) ([]agent.Event, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // the scene names its journal
	if err != nil {
		return nil, err
	}

	var events []agent.Event
	for number, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record struct {
			Event *agent.Event `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, number+1, err)
		}
		if record.Event != nil {
			events = append(events, *record.Event)
		}
	}

	return events, nil
}

func openTerminal(columns int, rows int) (*os.File, *os.File, error) {
	controller, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("no pseudo-terminal to run oh in: %w", err)
	}
	if err := unix.IoctlSetPointerInt(int(controller.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = controller.Close()
		return nil, nil, err
	}
	number, err := unix.IoctlGetInt(int(controller.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = controller.Close()
		return nil, nil, err
	}
	terminal, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = controller.Close()
		return nil, nil, err
	}
	size := &unix.Winsize{Row: uint16(rows), Col: uint16(columns)} //nolint:gosec // a scene names a size a terminal can have
	if err := unix.IoctlSetWinsize(int(terminal.Fd()), unix.TIOCSWINSZ, size); err != nil {
		_ = controller.Close()
		_ = terminal.Close()
		return nil, nil, err
	}

	return controller, terminal, nil
}

type transcript struct {
	mutex        sync.Mutex
	text         strings.Builder
	lastWriteAt  time.Time
	controller   *os.File
	answers      map[string]string
	answerCounts map[string]int
	isClosed     bool
}

func record(controller *os.File) *transcript {
	self := &transcript{
		lastWriteAt:  time.Now(),
		controller:   controller,
		answers:      map[string]string{},
		answerCounts: map[string]int{},
	}
	go self.readFrom(controller)

	return self
}

func (self *transcript) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.text.String()
}

func (self *transcript) answer(question string, reply string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.answers[question] = reply
}

func (self *transcript) readFrom(controller *os.File) {
	buffer := make([]byte, 64<<10)
	for {
		count, err := controller.Read(buffer)
		if count > 0 {
			self.mutex.Lock()
			self.text.Write(buffer[:count])
			self.lastWriteAt = time.Now()
			text := self.text.String()
			for question, reply := range self.answers {
				askCount := strings.Count(text, question)
				for ; self.answerCounts[question] < askCount; self.answerCounts[question]++ {
					_, _ = controller.WriteString(reply)
				}
			}
			self.mutex.Unlock()
		}
		if err != nil {
			self.mutex.Lock()
			self.isClosed = true
			self.mutex.Unlock()
			return
		}
	}
}

func (self *transcript) settle(quiet time.Duration, deadline time.Duration) bool {
	giveUpAt := time.Now().Add(deadline)
	time.Sleep(quiet)
	for time.Now().Before(giveUpAt) {
		self.mutex.Lock()
		stillFor := time.Since(self.lastWriteAt)
		isClosed := self.isClosed
		hasWritten := self.text.Len() > 0
		self.mutex.Unlock()

		if isClosed {
			return false
		}
		if hasWritten && stillFor >= quiet {
			return true
		}
		time.Sleep(quiet / 4)
	}

	return false
}

func (self *transcript) tail() string {
	text := self.String()
	const tailBytes = 2000
	if len(text) > tailBytes {
		text = text[len(text)-tailBytes:]
	}

	return text
}

var errUnknownKey = errors.New("unknown key")
