package grep

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/sandbox"
	"crdx.org/oh/pkg/tool"
)

type Args struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}

var matchPathPattern = regexp.MustCompile(`^(.+):[0-9]+:`)

func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
	return NewWithRunner(root, snapshots, sandbox.DirectArgv())
}

func NewWithRunner(root *file.Root, snapshots *file.Snapshots, runner sandbox.ArgvRunner) tool.Tool {
	if runner == nil {
		panic("grep needs a sandbox runner")
	}
	restoreReadState := func(payload json.RawMessage) error {
		return snapshots.RestoreReadState(root, payload)
	}

	return tool.Implement(
		tool.Definition{
			Name:        "grep",
			Description: "search file contents",
			Schema: tool.Schema{
				tool.String("pattern", "regexp"),
				tool.String("path", "directory, defaults to working directory").Optional(),
				tool.String("glob", "path filter, e.g. **/*.go").Optional(),
			},
		},
		Describe,
	).
		State(file.FileReadState, restoreReadState).
		Focuses(util.SearchPath).
		Syntax("regexp").
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Run(func(ctx context.Context, args Args) (tool.ToolCallResult, error) {
			output, metrics, err := run(ctx, root, args, runner)
			return tool.ToolCallResult{
				Output:  output,
				Metrics: metrics,
				State:   readStateForMatches(root, args, output),
			}, err
		})
}

func Describe(args Args) tool.CallRendering {
	subject, qualifier := util.DescribeSearch(args.Pattern, args.Path, args.Glob)
	return tool.CallRendering{Subject: subject, Qualifier: qualifier}
}

func confined(root *file.Root, name string) error {
	if _, err := root.Stat(name); err != nil {
		if pathError, ok := errors.AsType[*fs.PathError](err); ok {
			return fmt.Errorf("%s: %w", name, pathError.Err)
		}

		return err
	}

	return nil
}

const searchTimeout = 4*time.Minute + 30*time.Second

func run(ctx context.Context, root *file.Root, args Args, runner sandbox.ArgvRunner) (string, tool.ToolCallMetrics, error) {
	if args.Pattern == "" {
		return "", tool.ToolCallMetrics{}, errors.New("pattern is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}
	if err := confined(root, name); err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	binary, err := exec.LookPath("rg")
	if err != nil {
		return "", tool.ToolCallMetrics{}, fmt.Errorf("ripgrep is unavailable: %w", err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	arguments := []string{
		binary,
		"--no-config",
		"--with-filename",
		"--line-number",
		"--no-heading",
		"--color=never",
		"--hidden",
		"--no-require-git",
		"--glob=!.git/**",
		"--no-messages",
		"--max-columns=250",
	}
	if args.Glob != "" {
		arguments = append(arguments, "--glob="+args.Glob)
	}
	for _, pattern := range root.ExcludedNames() {
		arguments = append(arguments, "--glob=!"+pattern)
	}
	arguments = append(arguments, "--regexp="+args.Pattern, "--", name)

	searchContext, stopSearch := context.WithCancel(ctx)
	defer stopSearch()
	output := newSearchOutput(stopSearch)
	policy := sandbox.Policy{
		Read:    []string{root.Name()},
		Exec:    []string{binary},
		Env:     []string{"LANG", "TERM"},
		Timeout: searchTimeout,
	}
	command, err := runner.StartArgv(searchContext, root.Name(), arguments, policy, output)
	if err != nil {
		return "", tool.ToolCallMetrics{}, fmt.Errorf("could not start the search: %w", err)
	}
	result, waitErr := command.Wait()
	if ctx.Err() != nil {
		return "", tool.ToolCallMetrics{}, stop.Error(ctx, "the search")
	}
	if !output.IsTruncated() && waitErr != nil {
		return "", tool.ToolCallMetrics{}, waitErr
	}
	if !output.IsTruncated() && result.ExitCode != 0 {
		message := strings.TrimSpace(result.Output)
		if result.ExitCode == 1 && message == "" {
			answer, metrics := util.SearchReport(nil, false)
			return answer, metrics, nil
		}
		if strings.HasPrefix(message, "rg: regex parse error:") {
			return "", tool.ToolCallMetrics{}, fmt.Errorf("invalid pattern: %s", strings.TrimPrefix(message, "rg: "))
		}
		if message != "" {
			return "", tool.ToolCallMetrics{}, errors.New(message)
		}
		return "", tool.ToolCallMetrics{}, fmt.Errorf("grep failed with exit code %d", result.ExitCode)
	}

	readableOutput := result.Output
	if output.IsTruncated() {
		if lastNewline := strings.LastIndexByte(readableOutput, '\n'); lastNewline >= 0 {
			readableOutput = readableOutput[:lastNewline+1]
		} else {
			readableOutput = ""
		}
	}
	matches, reachedLimit, err := readMatches(strings.NewReader(readableOutput), name == ".")
	if err != nil {
		return "", tool.ToolCallMetrics{}, fmt.Errorf("failed to read ripgrep output: %w", err)
	}
	answer, metrics := util.SearchReport(matches, output.IsTruncated() || reachedLimit)
	return answer, metrics, nil
}

func readStateForMatches(root *file.Root, args Args, output string) json.RawMessage {
	searchRoot, _, err := root.Resolve(args.Path)
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	var readSnapshots []file.ReadSnapshot
	for line := range strings.Lines(output) {
		match := matchPathPattern.FindStringSubmatch(line)
		if match == nil || seen[match[1]] {
			continue
		}
		seen[match[1]] = true

		content, err := searchRoot.ReadFile(match[1])
		if err != nil {
			continue
		}

		path := match[1]
		if searchRoot != root {
			path = filepath.Join(searchRoot.Name(), path)
		}
		readSnapshots = append(readSnapshots, file.NewReadSnapshot(path, content))
	}

	if len(readSnapshots) == 0 {
		return nil
	}
	return file.EncodeReadState(readSnapshots...)
}

func readMatches(reader io.Reader, shouldTrimWorkingDirectory bool) ([]string, bool, error) {
	bufferedReader := bufio.NewReader(reader)
	var matches []string
	returnedBytes := int64(0)

	for {
		line, err := bufferedReader.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			if shouldTrimWorkingDirectory {
				line = strings.TrimPrefix(line, "./")
			}

			var isTruncated bool
			matches, returnedBytes, isTruncated = util.AppendSearchResult(matches, returnedBytes, line)
			if isTruncated {
				return matches, true, nil
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return matches, false, nil
			}

			return nil, false, err
		}
	}
}
