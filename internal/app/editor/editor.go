package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type Command []string

type Config struct {
	mutex   sync.RWMutex
	command Command
}

func NewConfiguration(command Command) *Config {
	return &Config{command: slices.Clone(command)}
}

func (self *Config) GetCommand() Command {
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return slices.Clone(self.command)
}

func (self *Config) ReplaceCommand(command Command) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.command = slices.Clone(command)
}

func (self *Command) UnmarshalTOML(value any) error {
	switch configuredValue := value.(type) {
	case string:
		*self = Command{strings.TrimSpace(configuredValue)}
		return nil
	case []any:
		command := make(Command, len(configuredValue))
		for i, argument := range configuredValue {
			text, ok := argument.(string)
			if !ok {
				return fmt.Errorf("editor argument %d is not a string", i+1)
			}
			command[i] = text
		}
		if len(command) > 0 {
			command[0] = strings.TrimSpace(command[0])
		}
		*self = command
		return nil
	default:
		return errors.New("editor is not a string or array of strings")
	}
}

var ErrNotFound = errors.New("no editor was found: set editor.command in config.toml, or $EDITOR")

type Launch struct {
	Command    Command
	IsTerminal bool
	Position   Position
}

type Position struct {
	Line       int
	Column     int
	ByteColumn int
}

func PositionIn(text []rune, cursor int) Position {
	cursor = max(0, min(cursor, len(text)))
	lineStart := 0
	line := 1
	for at, character := range text[:cursor] {
		if character == '\n' {
			line++
			lineStart = at + 1
		}
	}

	before := text[lineStart:cursor]
	return Position{Line: line, Column: len(before) + 1, ByteColumn: len(string(before)) + 1}
}

var placements = map[string]func(path string, position Position) []string{
	"vim":    placeByCall,
	"nvim":   placeByCall,
	"nano":   placeByComma,
	"ne":     placeByComma,
	"emacs":  placeByDisplayColumn,
	"micro":  placeByPlus,
	"hx":     placeBySuffix,
	"helix":  placeBySuffix,
	"subl":   placeBySuffix,
	"zed":    placeBySuffix,
	"code":   placeByGoto,
	"vi":     placeByLine,
	"joe":    placeByLine,
	"mcedit": placeByLine,
}

func placeByCall(path string, position Position) []string {
	return []string{fmt.Sprintf("+call cursor(%d, %d)", position.Line, position.ByteColumn), path}
}

func placeByComma(path string, position Position) []string {
	return []string{fmt.Sprintf("+%d,%d", position.Line, position.Column), path}
}

func placeByGoto(path string, position Position) []string {
	return append([]string{"--goto"}, placeBySuffix(path, position)...)
}

func placeByDisplayColumn(path string, position Position) []string {
	return []string{fmt.Sprintf("+%d:%d", position.Line, position.Column-1), path}
}

func placeByLine(path string, position Position) []string {
	return []string{fmt.Sprintf("+%d", position.Line), path}
}

func placeByPlus(path string, position Position) []string {
	return []string{fmt.Sprintf("+%d:%d", position.Line, position.Column), path}
}

func placeBySuffix(path string, position Position) []string {
	return []string{fmt.Sprintf("%s:%d:%d", path, position.Line, position.Column)}
}

func (self Launch) Name() string {
	return filepath.Base(self.Command[0])
}

type Outcome struct {
	Launch    Launch
	Paths     []string
	IsAwaited bool
	Failure   error
}

type Terminal struct {
	Input  *os.File
	Output *os.File
}

type candidate struct {
	command      Command
	isTerminal   bool
	needsDisplay bool
}

var preferredEditor = candidate{command: Command{"subl", "--wait"}, needsDisplay: true}

var fallbackEditors = []candidate{
	{command: Command{"vim"}, isTerminal: true},
	{command: Command{"code", "--wait"}, needsDisplay: true},
	{command: Command{"zed", "--wait"}, needsDisplay: true},
	{command: Command{"nvim"}, isTerminal: true},
	{command: Command{"hx"}, isTerminal: true},
	{command: Command{"helix"}, isTerminal: true},
	{command: Command{"micro"}, isTerminal: true},
	{command: Command{"nano"}, isTerminal: true},
	{command: Command{"emacs", "-nw"}, isTerminal: true},
	{command: Command{"kak"}, isTerminal: true},
	{command: Command{"ne"}, isTerminal: true},
	{command: Command{"joe"}, isTerminal: true},
	{command: Command{"mcedit"}, isTerminal: true},
	{command: Command{"vi"}, isTerminal: true},
}

var editorVariables = []string{"VISUAL", "EDITOR"}

var displayVariables = []string{"DISPLAY", "WAYLAND_DISPLAY"}

var terminalEditors = []string{
	"ed",
	"emacs",
	"helix",
	"hx",
	"jed",
	"joe",
	"kak",
	"mcedit",
	"micro",
	"nano",
	"ne",
	"nvim",
	"pico",
	"vi",
	"view",
	"vim",
}

func IsTerminalEditor(name string) bool {
	return slices.Contains(terminalEditors, filepath.Base(name))
}

func Resolve(configuredCommand Command, getenv func(string) string) (Launch, error) {
	if len(configuredCommand) > 0 && strings.TrimSpace(configuredCommand[0]) != "" {
		command := slices.Clone(configuredCommand)
		command[0] = strings.TrimSpace(command[0])
		return Launch{Command: command, IsTerminal: IsTerminalEditor(command[0])}, nil
	}

	isDisplayed := hasDisplay(getenv)
	if preferredEditor.isUsable(isDisplayed) {
		return preferredEditor.launch(), nil
	}

	for _, variable := range editorVariables {
		if command := strings.Fields(getenv(variable)); len(command) > 0 {
			return Launch{Command: command, IsTerminal: true}, nil
		}
	}

	for _, fallback := range fallbackEditors {
		if fallback.isUsable(isDisplayed) {
			return fallback.launch(), nil
		}
	}

	return Launch{}, ErrNotFound
}

func (self candidate) isUsable(isDisplayed bool) bool {
	return (isDisplayed || !self.needsDisplay) && isInstalled(self.command[0])
}

func (self candidate) launch() Launch {
	return Launch{Command: slices.Clone(self.command), IsTerminal: self.isTerminal}
}

func hasDisplay(getenv func(string) string) bool {
	return slices.ContainsFunc(displayVariables, func(variable string) bool { return getenv(variable) != "" })
}

func isInstalled(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (self Launch) Arguments(paths []string) []string {
	arguments := slices.Clone(self.Command[1:])
	if place, isPlaceable := placements[self.Name()]; isPlaceable && self.Position.Line > 0 && len(paths) == 1 {
		return append(arguments, place(paths[0], self.Position)...)
	}
	if !self.IsTerminal {
		return append(arguments, paths...)
	}

	files := slices.DeleteFunc(slices.Clone(paths), isDirectory)
	if len(files) == 0 {
		return append(arguments, paths...)
	}

	return append(arguments, files...)
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (self Launch) Run(ctx context.Context, paths []string, terminal Terminal) error {
	if self.IsTerminal {
		return self.runInTerminal(ctx, paths, terminal)
	}

	return self.runApart(ctx, paths)
}

func (self Launch) command(ctx context.Context, paths []string) *exec.Cmd {
	//nolint:gosec // the person chose the editor
	return exec.CommandContext(ctx, self.Command[0], self.Arguments(paths)...)
}

const failureBytes = 4096

func (self Launch) runApart(ctx context.Context, paths []string) error {
	command := self.command(ctx, paths)
	failure := &tail{limit: failureBytes}
	command.Stderr = failure

	if err := command.Start(); err != nil {
		return fmt.Errorf("%s: %w", self.Name(), err)
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return describeExit(self.Name(), err, failure.lastLine())
	}

	return nil
}

func (self Launch) runInTerminal(ctx context.Context, paths []string, terminal Terminal) error {
	command := self.command(ctx, paths)
	command.Stdin = terminal.Input
	command.Stdout = terminal.Output
	command.Stderr = terminal.Output

	descriptor := int(terminal.Input.Fd())
	foreground, err := unix.IoctlGetInt(descriptor, unix.TIOCGPGRP)
	isForeground := err == nil && foreground == unix.Getpgrp()
	if isForeground {
		command.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: descriptor}
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("%s: %w", self.Name(), err)
	}
	err = command.Wait()
	if isForeground {
		takeForeground(descriptor, foreground)
	}
	if err != nil {
		return describeExit(self.Name(), err, "")
	}

	return nil
}

func takeForeground(descriptor int, processGroup int) {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)

	_ = unix.IoctlSetPointerInt(descriptor, unix.TIOCSPGRP, processGroup)
}

func describeExit(name string, err error, lastLine string) error {
	if lastLine != "" {
		return fmt.Errorf("%s: %s", name, lastLine)
	}

	return fmt.Errorf("%s: %w", name, err)
}

type tail struct {
	mutex sync.Mutex
	limit int
	bytes []byte
}

func (self *tail) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.bytes = append(self.bytes, data...)
	if excess := len(self.bytes) - self.limit; excess > 0 {
		self.bytes = self.bytes[excess:]
	}

	return len(data), nil
}

func (self *tail) lastLine() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	lines := strings.Split(strings.TrimSpace(string(self.bytes)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
