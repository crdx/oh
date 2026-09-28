package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/tool"
)

type Kind string

const (
	KindString  Kind = "string"
	KindInteger Kind = "integer"
	KindBoolean Kind = "boolean"
	KindStrings Kind = "strings"
	KindEnum    Kind = "enum"
)

var kinds = []Kind{KindString, KindInteger, KindBoolean, KindStrings, KindEnum}

const (
	defaultTimeLimit = 30 * time.Second
	waitDelay        = time.Second
	toolVariable     = "OH_TOOL"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var ErrCommandFailed = errors.New("the command failed")

type Parameter struct {
	Name        string
	Kind        Kind
	Description string
	Values      []string
	IsOptional  bool
}

type Declaration struct {
	Name          string
	Description   string
	Command       []string
	Parameters    []Parameter
	Subject       string
	TimeLimit     time.Duration
	MustAsk       bool
	Group         string
	Compatibility []Compatibility
}

type Compatibility struct {
	From string
	To   string
}

type Options struct {
	Directory      string
	Approve        func(ctx context.Context, name string, command string) error
	GroupForTool   func(name string) string
	IsGroupAllowed func(group string) bool
}

func New(declaration Declaration, options Options) (tool.Tool, error) {
	schema, err := declaration.buildSchema()
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(declaration.Description) == "" {
		return nil, errors.New("description is empty")
	}

	if len(declaration.Command) == 0 {
		return nil, errors.New("command is empty")
	}

	executable, err := exec.LookPath(declaration.Command[0])
	if err != nil {
		return nil, fmt.Errorf("could not find %s: %w", declaration.Command[0], err)
	}

	subject := declaration.Subject
	if subject == "" && len(schema) > 0 {
		subject = schema[0].Name
	}
	if subject != "" && schema.Find(subject) == nil {
		return nil, fmt.Errorf("subject names %s, which is not a parameter", subject)
	}

	timeLimit := declaration.TimeLimit
	if timeLimit <= 0 {
		timeLimit = defaultTimeLimit
	}
	compatibilityRevision, err := commandRevision(declaration, executable, timeLimit)
	if err != nil {
		return nil, fmt.Errorf("identify command: %w", err)
	}
	compatibleRevisions, err := acceptedRevisions(declaration.Compatibility, compatibilityRevision)
	if err != nil {
		return nil, err
	}

	builder := tool.Implement(
		tool.Definition{
			Name:        declaration.Name,
			Description: strings.TrimSpace(declaration.Description),
			Schema:      schema,
		},
		func(arguments tool.Arguments) tool.CallRendering {
			callSubject, qualifier := describe(arguments, subject)
			return tool.CallRendering{Subject: callSubject, Qualifier: qualifier}
		},
	).
		Decode(schema.Decode).
		Revision(compatibilityRevision).
		CompatibleWith(compatibleRevisions...).
		TakesAtMost(func(tool.Arguments) time.Duration { return timeLimit })

	if declaration.Group != "" {
		builder = builder.Requires(
			func() bool {
				return options.IsGroupAllowed != nil && options.IsGroupAllowed(declaration.Group)
			},
			fmt.Errorf(
				"%s access unavailable; ctrl+x %s grants it",
				declaration.Name,
				declaration.Group,
			),
		)
	}

	return builder.Plain(func(ctx context.Context, arguments tool.Arguments) (string, error) {
		return run(ctx, declaration, executable, arguments, options, timeLimit)
	}), nil
}

func acceptedRevisions(compatibility []Compatibility, currentRevision string) ([]string, error) {
	var revisions []string
	for _, transition := range compatibility {
		if !isRevision(transition.From) {
			return nil, fmt.Errorf("compatible.from %q is not a SHA-256 revision", transition.From)
		}
		if !isRevision(transition.To) {
			return nil, fmt.Errorf("compatible.to %q is not a SHA-256 revision", transition.To)
		}
		if transition.To == currentRevision {
			revisions = append(revisions, transition.From)
		}
	}
	slices.Sort(revisions)
	return slices.Compact(revisions), nil
}

func isRevision(value string) bool {
	decodedRevision, err := hex.DecodeString(value)
	return err == nil && len(decodedRevision) == sha256.Size
}

type commandFileIdentity struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type commandIdentity struct {
	Executable string                `json:"executable"`
	Command    []string              `json:"command"`
	Subject    string                `json:"subject"`
	TimeLimit  time.Duration         `json:"time_limit"`
	MustAsk    bool                  `json:"must_ask"`
	Files      []commandFileIdentity `json:"files,omitempty"`
}

func commandRevision(declaration Declaration, executable string, timeLimit time.Duration) (string, error) {
	identity := commandIdentity{
		Executable: executable,
		Command:    declaration.Command,
		Subject:    declaration.Subject,
		TimeLimit:  timeLimit,
		MustAsk:    declaration.MustAsk,
	}

	paths := []string{executable}
	for _, word := range declaration.Command[1:] {
		if filepath.IsAbs(word) {
			paths = append(paths, word)
		}
	}
	seenPaths := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, isSeen := seenPaths[path]; isSeen {
			continue
		}
		seenPaths[path] = struct{}{}

		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", path, err)
		}
		digest, err := digestFile(path)
		if err != nil {
			return "", err
		}
		identity.Files = append(identity.Files, commandFileIdentity{Path: path, Digest: digest})
	}

	encodedIdentity, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encodedIdentity)
	return hex.EncodeToString(digest[:]), nil
}

func digestFile(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // the user explicitly declared this command file
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (self Declaration) buildSchema() (tool.Schema, error) {
	if !namePattern.MatchString(self.Name) {
		return nil, fmt.Errorf("%q is not a usable tool name", self.Name)
	}

	schema := make(tool.Schema, 0, len(self.Parameters))

	for _, declaration := range self.Parameters {
		if !namePattern.MatchString(declaration.Name) {
			return nil, fmt.Errorf("%q is not a usable parameter name", declaration.Name)
		}
		if schema.Find(declaration.Name) != nil {
			return nil, fmt.Errorf("%s is declared twice", declaration.Name)
		}
		if strings.TrimSpace(declaration.Description) == "" {
			return nil, fmt.Errorf("%s has no description", declaration.Name)
		}

		parameter, err := declaration.build()
		if err != nil {
			return nil, err
		}

		schema = append(schema, parameter)
	}

	return schema, nil
}

func (self Parameter) build() (tool.Parameter, error) {
	if !slices.Contains(kinds, self.Kind) {
		return tool.Parameter{}, fmt.Errorf(
			"%s has kind %q, which is not one of: %s", self.Name, self.Kind, joinKinds(),
		)
	}

	if self.Kind != KindEnum && len(self.Values) > 0 {
		return tool.Parameter{}, fmt.Errorf("%s is not an enum, so it takes no values", self.Name)
	}

	var parameter tool.Parameter

	switch self.Kind {
	case KindString:
		parameter = tool.String(self.Name, self.Description)
	case KindInteger:
		parameter = tool.Integer(self.Name, self.Description)
	case KindBoolean:
		parameter = tool.Boolean(self.Name, self.Description)
	case KindStrings:
		parameter = tool.StringArray(self.Name, self.Description)
	case KindEnum:
		if len(self.Values) == 0 {
			return tool.Parameter{}, fmt.Errorf("%s is an enum, so it needs values", self.Name)
		}
		parameter = tool.Enum(self.Name, self.Description, self.Values...)
	}

	if self.IsOptional {
		parameter = parameter.Optional()
	}

	return parameter, nil
}

func joinKinds() string {
	names := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		names = append(names, string(kind))
	}

	return strings.Join(names, ", ")
}

func argv(executable string, command []string, arguments tool.Arguments) []string {
	line := make([]string, 0, len(command)+2*len(arguments.Schema()))
	line = append(line, executable)
	line = append(line, command[1:]...)

	for _, parameter := range arguments.Schema() {
		if !arguments.IsPresent(parameter.Name) {
			continue
		}

		option := "--" + strings.ReplaceAll(parameter.Name, "_", "-")

		switch parameter.Type {
		case tool.TypeBoolean:
			if arguments.GetBoolean(parameter.Name) {
				line = append(line, option)
			}
		case tool.TypeArray:
			for _, item := range arguments.GetStrings(parameter.Name) {
				line = append(line, option, item)
			}
		case tool.TypeString, tool.TypeInteger, tool.TypeObject:
			line = append(line, option, arguments.GetText(parameter.Name))
		}
	}

	return line
}

func render(line []string) string {
	words := make([]string, 0, len(line))

	for _, word := range line {
		quotation, err := syntax.Quote(word, syntax.LangBash)
		if err != nil {
			quotation = word
		}
		words = append(words, quotation)
	}

	return strings.Join(words, " ")
}

func describe(arguments tool.Arguments, subject string) (string, string) {
	var values []string

	for _, parameter := range arguments.Schema() {
		if parameter.Name == subject || !arguments.IsPresent(parameter.Name) {
			continue
		}

		if parameter.Type == tool.TypeBoolean {
			if arguments.GetBoolean(parameter.Name) {
				values = append(values, parameter.Name)
			}

			continue
		}

		if text := strings.TrimSpace(arguments.GetText(parameter.Name)); text != "" {
			values = append(values, text)
		}
	}

	if subject == "" {
		return strings.Join(values, " "), ""
	}

	return strutil.FirstLine(arguments.GetText(subject)), strutil.FirstLine(strings.Join(values, " "))
}

func run(
	ctx context.Context,
	declaration Declaration,
	executable string,
	arguments tool.Arguments,
	options Options,
	timeLimit time.Duration,
) (string, error) {
	line := argv(executable, declaration.Command, arguments)

	if declaration.MustAsk && options.Approve != nil {
		if err := options.Approve(ctx, declaration.Name, render(line)); err != nil {
			return "", err
		}
	}

	runContext, cancel := context.WithTimeout(ctx, timeLimit)
	defer cancel()

	//nolint:gosec // the person declared this executable themselves, and every argument is its own word
	process := exec.CommandContext(runContext, line[0], line[1:]...)
	process.Dir = options.Directory
	process.Stdin = nil
	process.WaitDelay = waitDelay
	process.Env = append(process.Environ(), toolVariable+"="+declaration.Name)

	output, err := process.CombinedOutput()
	text := strings.TrimRight(string(output), "\n")

	if errors.Is(runContext.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return status(text, "stopped after its limit of "+timeLimit.String()), ErrCommandFailed
	}

	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return status(text, fmt.Sprintf("exit(%d)", exit.ExitCode())), ErrCommandFailed
	case err != nil:
		return "", err
	}

	return text, nil
}

func status(output string, note string) string {
	if strings.TrimSpace(output) == "" {
		return note
	}

	return note + ":\n" + output
}
