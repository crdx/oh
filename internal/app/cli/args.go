package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/toolset"
)

const (
	stdinMarker      = "-"
	optionTerminator = "--"
	defaultCapFlags  = "rx"
	yoloOption       = "--yolo"
)

var usage = `
Usage:
    $0 [options] [-t <tool>]... [<prompt>...]
    $0 --login [<provider>]
    $0 --usage [--json]
    $0 --update [--ignored]
    $0 --ctl <command> [<args>...]

Options:
    -r, --resume [<session>]    Resume a stored session
    -f, --from <session>        Fork a previous session
    -m, --model [<model>]       Pick which model to use
    -c, --caps <flags>          Configure access rights
    -t, --tool <tool>           Replace current toolbox
    -p, --print                 Stream in headless mode
        --ctl                   Run maintenance command
        --demo                  Follow the white rabbit
        --yolo                  Run outside the sandbox
    -l, --list                  Show every usable model
    -u, --update                Refresh the model cache
    -L, --login                 Control provider logins
    -U, --usage                 View subscription usage
    -I, --ignored               Display ignored entries
    -J, --json                  Format output into JSON
    -v, --version               Print this version info
    -h, --help                  Print this help message
`

type inputFlags struct {
	IsControlling    bool     `docopt:"--ctl"`
	ControlCommand   string   `docopt:"<command>"`
	ControlArguments []string `docopt:"<args>"`
	Message          []string `docopt:"<prompt>"`
	Login            bool     `docopt:"--login"`
	Provider         string   `docopt:"<provider>"`
	Session          string   `docopt:"--resume"`
	IsSessionPicker  bool     `docopt:"-r"`
	SourceSession    string   `docopt:"--from"`
	Model            string   `docopt:"--model"`
	IsModelPicker    bool     `docopt:"-m"`
	Caps             string   `docopt:"--caps"`
	Tools            []string `docopt:"--tool"`
	IsPrinting       bool     `docopt:"--print"`
	IsDemoing        bool     `docopt:"--demo"`
	Usage            bool     `docopt:"--usage"`
	JSON             bool     `docopt:"--json"`
	Yolo             bool     `docopt:"--yolo"`
	List             bool     `docopt:"--list"`
	Update           bool     `docopt:"--update"`
	IsShowingIgnored bool     `docopt:"--ignored"`
	Version          bool     `docopt:"--version"`
}

type Input struct {
	inputFlags
}

type Options struct {
	Message        string
	Session        string
	SourceSession  string
	Selection      model.Selection
	Caps           caps.Set
	GroupFlags     string
	WereCapsChosen bool
	Tools          []string
	AddedFiles     []startup.InitialFile
	Yolo           bool
	IsPrinting     bool
}

func Bind() *Input {
	originalArgs := os.Args
	arguments := append([]string(nil), os.Args...)
	isSessionPicker := false
	isModelPicker := false
	for i := 1; i < len(arguments); i++ {
		if arguments[i] == optionTerminator {
			break
		}

		switch {
		case (arguments[i] == "-r" || arguments[i] == "--resume") && (i+1 == len(arguments) || strings.HasPrefix(arguments[i+1], "-")):
			isSessionPicker = true
			arguments = append(arguments[:i], arguments[i+1:]...)
			i--
		case (arguments[i] == "-m" || arguments[i] == "--model") && (i+1 == len(arguments) || strings.HasPrefix(arguments[i+1], "-")):
			isModelPicker = true
			arguments = append(arguments[:i], arguments[i+1:]...)
			i--
		case arguments[i] == "-r" && i+1 < len(arguments) && !strings.HasPrefix(arguments[i+1], "-"):
			arguments[i] = "--resume"
		}
	}

	os.Args = arguments
	defer func() { os.Args = originalArgs }()

	parsedFlags := duckopt.MustBind[inputFlags](usage, "$0")
	parsedFlags.IsSessionPicker = isSessionPicker
	parsedFlags.IsModelPicker = isModelPicker
	parsedFlags.Message = promptWithoutOptionTerminator(parsedFlags.Message)
	parsedFlags.Message = promptAfterStdinMarker(parsedFlags.Message)
	return &Input{inputFlags: *parsedFlags}
}

func promptWithoutOptionTerminator(words []string) []string {
	index := slices.Index(words, optionTerminator)
	if index == -1 {
		return words
	}

	return slices.Delete(words, index, index+1)
}

func promptAfterStdinMarker(words []string) []string {
	if len(words) > 0 && words[0] == stdinMarker {
		return words[1:]
	}

	return words
}

func (self Options) Resuming() bool {
	return self.Session != ""
}

func (self Options) StartingFromSession() bool {
	return self.SourceSession != ""
}

func (self Input) Parse(
	choices []model.Choice,
	defaults model.Defaults,
	customGroupFlags ...string,
) (Options, error) {
	options := Options{
		Message:       strings.Join(self.Message, " "),
		Session:       self.Session,
		SourceSession: self.SourceSession,
		Tools:         self.Tools,
		Yolo:          self.Yolo,
		IsPrinting:    self.IsPrinting,
	}

	if self.Model != "" {
		selection, err := model.ParseSelection(choices, self.Model, defaults)
		if err != nil {
			return options, err
		}
		options.Selection = selection
	}

	if self.Yolo {
		if err := RefuseConfinedCaps(self.Caps); err != nil {
			return options, err
		}
	}

	capFlags := self.Caps
	if capFlags == "" {
		capFlags = defaultCapFlags
	}

	allowedGroups := ""
	if len(customGroupFlags) > 0 {
		allowedGroups = customGroupFlags[0]
	}
	if options.Resuming() {
		allowedGroups = "abcdefghijklmnopqrstuvwxyz"
	}
	grantedCaps, grantedGroups, err := caps.ParseWithGroups(capFlags, allowedGroups)
	if err != nil {
		return options, err
	}
	options.Caps = grantedCaps
	options.GroupFlags = grantedGroups
	options.WereCapsChosen = self.Caps != ""

	if options.Resuming() && options.StartingFromSession() {
		return options, errors.New("a conversation cannot be resumed while another session supplies its context")
	}

	return options, nil
}

func RefuseConfinedCaps(flags string) error {
	for _, flag := range flags {
		if knownCap, isBuiltIn := caps.Named(string(flag)); isBuiltIn && caps.Unconfined().Has(knownCap) {
			return fmt.Errorf(
				"--yolo leaves %s always on, so --caps takes only %s and custom tool groups (got %q)",
				caps.Unconfined().Flags(),
				caps.Lookup.Flag(),
				flags,
			)
		}
	}

	return nil
}

func (self Input) Check(isPromptPiped bool) error {
	if self.isResuming() && self.isChoosingModel() {
		return errors.New(
			"a resumed conversation preserves its model; start a new session to choose another",
		)
	}

	if self.isResuming() && len(self.Tools) > 0 {
		return errors.New(
			"a resumed conversation preserves its toolbox; start a new session to change them",
		)
	}

	if self.IsDemoing {
		if self.isResuming() || self.SourceSession != "" {
			return errors.New("the simulation keeps nothing, so there is no session of its to resume")
		}

		if self.isChoosingModel() {
			return errors.New("the simulation answers in place of a model, so there is none to choose")
		}
	}

	if !self.IsPrinting {
		return nil
	}

	if self.IsSessionPicker || self.IsModelPicker {
		return errors.New("a headless session cannot open a picker; name the session or the model instead")
	}

	if err := toolset.RefuseOutsideHeadless(self.Tools); err != nil {
		return err
	}

	if len(self.Message) == 0 && self.SourceSession == "" && !isPromptPiped {
		return errors.New("a headless session needs a prompt")
	}

	return nil
}

func (self Input) isResuming() bool {
	return self.IsSessionPicker || self.Session != ""
}

func (self Input) isChoosingModel() bool {
	return self.IsModelPicker || self.Model != ""
}

func InheritedOptions(arguments []string, transition cycle.Transition) []string {
	if transition.Kind != cycle.NewSession || slices.Contains(transition.Arguments, yoloOption) {
		return nil
	}

	if IsYoloInherited(arguments) {
		return []string{yoloOption}
	}

	return nil
}

func IsYoloInherited(arguments []string) bool {
	return slices.Contains(arguments, yoloOption)
}

func OptionDescription(name string) string {
	for line := range strings.SplitSeq(usage, "\n") {
		names, description, hasDescription := strings.Cut(strings.TrimSpace(line), "  ")
		if !hasDescription {
			continue
		}
		for field := range strings.FieldsSeq(names) {
			if strings.TrimSuffix(field, ",") == name {
				return strings.TrimSpace(description)
			}
		}
	}

	return ""
}
