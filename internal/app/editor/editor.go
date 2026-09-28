package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"crdx.org/oh/internal/app/style"
)

type Command []string

type Config struct {
	mutex   sync.RWMutex
	command Command
}

type ConfigurationFiles struct {
	DefaultsContents string
	DefaultsPath     string
	Directory        string
	UserPath         string
	UserContents     string
	AdditionalPaths  []string
}

type sublimeSettingsArguments struct {
	BaseFile string `json:"base_file"`
	UserFile string `json:"user_file"`
	Default  string `json:"default"`
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

func (self *Config) Open(paths ...string) error {
	return Open(self.GetCommand(), paths...)
}

func (self *Config) OpenConfiguration(files ConfigurationFiles) error {
	return OpenConfiguration(self.GetCommand(), files)
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

func Open(configuredCommand Command, paths ...string) error {
	command, err := buildCommand(configuredCommand, paths)
	if err != nil {
		return err
	}
	return start(command)
}

func OpenConfiguration(configuredCommand Command, files ConfigurationFiles) error {
	commands, usesDefaults, err := buildConfigurationCommands(configuredCommand, files)
	if err != nil {
		return err
	}
	if !usesDefaults {
		return start(commands[0])
	}

	if err := materialiseDefaults(files.DefaultsPath, files.DefaultsContents); err != nil {
		return fmt.Errorf("could not prepare default config: %w", err)
	}
	if err := run(commands[0]); err != nil {
		return fmt.Errorf("could not open Sublime settings: %w", err)
	}
	for _, command := range commands[1:] {
		if err := start(command); err != nil {
			return err
		}
	}
	return nil
}

var candidates = []Command{
	{"subl", "--wait"},
	{"code", "--wait"},
}

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

func Detect() (Command, bool) {
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return candidate, true
		}
	}

	return nil, false
}

func IsTerminalEditor(name string) bool {
	return slices.Contains(terminalEditors, filepath.Base(name))
}

func buildCommand(configuredCommand Command, paths []string) (*exec.Cmd, error) {
	command, err := resolveCommand(configuredCommand)
	if err != nil {
		return nil, err
	}
	return executableCommand(command, paths), nil
}

func buildConfigurationCommands(
	configuredCommand Command,
	files ConfigurationFiles,
) ([]*exec.Cmd, bool, error) {
	command, err := resolveCommand(configuredCommand)
	if err != nil {
		return nil, false, err
	}
	if !isSublime(command[0]) {
		paths := []string{files.Directory}
		paths = append(paths, files.AdditionalPaths...)
		paths = append(paths, files.UserPath)
		return []*exec.Cmd{executableCommand(command, paths)}, false, nil
	}

	encodedArguments, err := json.Marshal(sublimeSettingsArguments{
		BaseFile: files.DefaultsPath,
		UserFile: files.UserPath,
		Default:  files.UserContents + "$0",
	})
	if err != nil {
		return nil, false, err
	}
	arguments := slices.DeleteFunc(slices.Clone(command[1:]), isWaitArgument)
	arguments = append(arguments, "--command", "edit_settings "+string(encodedArguments))
	commands := []*exec.Cmd{executableCommand(Command{command[0]}, arguments)}
	if len(files.AdditionalPaths) != 0 {
		commands = append(commands, executableCommand(command, files.AdditionalPaths))
	}
	return commands, true, nil
}

func resolveCommand(configuredCommand Command) (Command, error) {
	if len(configuredCommand) == 0 || strings.TrimSpace(configuredCommand[0]) == "" {
		detectedCommand, found := Detect()
		if !found {
			return nil, errors.New("no editor was found: set editor in config.toml")
		}
		configuredCommand = detectedCommand
	}

	command := slices.Clone(configuredCommand)
	command[0] = strings.TrimSpace(command[0])
	if IsTerminalEditor(command[0]) {
		return nil, fmt.Errorf(
			"%s is not supported yet: set a graphical editor in config.toml",
			command[0],
		)
	}
	return command, nil
}

func executableCommand(command Command, arguments []string) *exec.Cmd {
	arguments = append(slices.Clone(command[1:]), arguments...)
	//nolint:gosec,noctx // the user configures the editor, and it outlives this call
	return exec.Command(command[0], arguments...)
}

func isSublime(name string) bool {
	return slices.Contains([]string{"subl", "sublime_text"}, filepath.Base(name))
}

func isWaitArgument(argument string) bool {
	return argument == "-w" || argument == "--wait"
}

func start(command *exec.Cmd) error {
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("could not start editor: %w", err)
	}
	go reportExit(command, os.Stderr)
	return nil
}

func run(command *exec.Cmd) error {
	command.Stderr = os.Stderr
	return command.Run()
}

func materialiseDefaults(path string, contents string) error {
	current, err := os.ReadFile(path) //nolint:gosec // the application selects its cache path
	if err == nil && bytes.Equal(current, []byte(contents)) {
		return os.Chmod(path, 0o400)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".defaults-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if _, err := io.WriteString(temporary, contents); err != nil {
		return err
	}
	if err := temporary.Chmod(0o400); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func reportExit(command *exec.Cmd, errors io.Writer) {
	if err := command.Wait(); err != nil {
		_, _ = fmt.Fprintln(errors, style.Error(fmt.Errorf("editor exited: %w", err)))
	}
}
