package bash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/tool"
)

type Network string

const (
	LoopbackNetwork Network = "loopback"
	HostNetwork     Network = "host"
)

type Args struct {
	Command string  `json:"command"`
	Network Network `json:"network,omitempty"`
}

func New(
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	approveNetwork func(context.Context, string) error,
	runner sandbox.Runner,
	hasNetworkChoice bool,
) tool.Tool {
	schema := tool.Schema{tool.String("command", "the command line")}
	if hasNetworkChoice {
		schema = append(schema, tool.Enum(
			"network",
			"the networking state available to run on: loopback (default, your sandbox env) or host (the user's)",
			string(LoopbackNetwork),
			string(HostNetwork),
		).Optional())
	}

	return tool.Implement(
		tool.Definition{
			Name:        "bash",
			Description: "run a shell command",
			Schema:      schema,
		},
		Describe,
	).
		Validate(func(args Args) error { return validate(args, hasNetworkChoice) }).
		SyntaxFrom("bash", emphasisSource).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			isHostNetwork := hasNetworkChoice && args.Network == HostNetwork
			if isHostNetwork {
				if err := approveNetwork(ctx, args.Command); err != nil {
					return "", tool.ToolCallMetrics{}, err
				}
			}
			policy, err := buildPolicy(ctx)
			if err != nil {
				return "", tool.ToolCallMetrics{}, err
			}
			policy.Network = isHostNetwork
			return exec(ctx, runner, root, policy, args)
		})
}

func ProtectedPolicy(policy sandbox.Policy) sandbox.Policy {
	var readOnlyPaths []string
	for _, path := range policy.Write {
		gitDir := filepath.Join(path, ".git")

		if pathutil.Exists(gitDir) && !slices.Contains(policy.Read, gitDir) && !slices.Contains(readOnlyPaths, gitDir) {
			readOnlyPaths = append(readOnlyPaths, gitDir)
		}
	}

	return policy.WithRead(readOnlyPaths...)
}

func Describe(args Args) (string, string) {
	rendering := DescribeCommand(args.Command)
	return rendering.Subject, rendering.Qualifier
}

func DescribeCommand(command string) tool.CallRendering {
	var subject string
	parsedScript, err := parse(command)
	switch {
	case err != nil:
		subject = oneLine(command)
	case hasHereDocument(parsedScript):
		subject = strutil.FirstLine(command)
	default:
		subject = format(parsedScript)
	}

	return tool.CallRendering{
		Name:      "bash",
		Subject:   subject,
		Qualifier: spread(command),
		Emphasis: tool.Emphasis{
			Kind:   tool.EmphasisSyntax,
			Value:  "bash",
			Source: emphasisSource(Args{Command: command}, subject),
		},
	}
}

func emphasisSource(args Args, subject string) string {
	source := strings.TrimSpace(args.Command)
	if strings.ContainsAny(source, "\r\n") && strings.HasPrefix(source, subject) {
		return source
	}

	return ""
}

func hasHereDocument(parsedScript *syntax.File) bool {
	isFound := false

	syntax.Walk(parsedScript, func(node syntax.Node) bool {
		if redirect, ok := node.(*syntax.Redirect); ok && redirect.Hdoc != nil {
			isFound = true
		}

		return !isFound
	})

	return isFound
}

func validate(args Args, hasNetworkChoice bool) error {
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("command is required")
	}

	if _, err := parse(args.Command); err != nil {
		return fmt.Errorf("invalid Bash command: %w", err)
	}

	if !hasNetworkChoice {
		return nil
	}

	switch args.Network {
	case "", LoopbackNetwork, HostNetwork:
	default:
		return fmt.Errorf(
			"network must be %q or %q (got %q)",
			LoopbackNetwork, HostNetwork, args.Network,
		)
	}

	return nil
}

func Steps(command string) []string {
	parsedScript, err := parse(command)
	if err != nil || len(parsedScript.Stmts) != 1 {
		return []string{command}
	}

	var steps []string

	var walk func(statement *syntax.Stmt)
	walk = func(statement *syntax.Stmt) {
		binary, isBinary := statement.Cmd.(*syntax.BinaryCmd)
		if !isBinary || (binary.Op != syntax.AndStmt && binary.Op != syntax.OrStmt) {
			steps = append(steps, command[statement.Pos().Offset():statement.End().Offset()])
			return
		}

		walk(binary.X)
		steps[len(steps)-1] += " " + binary.Op.String()
		walk(binary.Y)
	}
	walk(parsedScript.Stmts[0])

	sentMeaning, sentErr := canonical(command)
	stepMeaning, stepErr := canonical(strings.Join(steps, "\n"))
	if sentErr != nil || stepErr != nil || sentMeaning != stepMeaning {
		return []string{command}
	}

	return steps
}

func canonical(command string) (string, error) {
	parsedScript, err := parse(command)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := syntax.NewPrinter(syntax.SingleLine(true)).Print(&out, parsedScript); err != nil {
		return "", err
	}

	return out.String(), nil
}

func parse(command string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
}

func format(parsedScript *syntax.File) string {
	command := printScript(parsedScript, syntax.SingleLine(true))
	if _, err := parse(command); err == nil {
		return command
	}

	return oneLine(printScript(parsedScript))
}

func printScript(parsedScript *syntax.File, options ...syntax.PrinterOption) string {
	var output bytes.Buffer
	if err := syntax.NewPrinter(options...).Print(&output, parsedScript); err != nil {
		panic(err)
	}

	return strings.TrimSuffix(output.String(), "\n")
}

func oneLine(command string) string {
	var out strings.Builder
	isSeparated := true

	for line := range strings.FieldsFuncSeq(command, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}

		if out.Len() > 0 {
			if isSeparated {
				out.WriteByte(' ')
			} else {
				out.WriteString("; ")
			}
		}

		out.WriteString(line)
		isSeparated = hasSeparator(line)
	}

	return out.String()
}

func hasSeparator(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}

	if strings.ContainsRune(";&|<>", rune(line[len(line)-1])) || strings.HasSuffix(line, `\`) {
		return true
	}

	fields := strings.Fields(line)
	last := fields[len(fields)-1]

	return last == "then" || last == "do" || last == "else" || last == "in" || last == "{" || last == "("
}

func spread(command string) string {
	if lines := strings.Count(strings.TrimRight(command, "\n"), "\n") + 1; lines > 1 {
		return fmt.Sprintf("%dL", lines)
	}

	return ""
}

func exec(
	ctx context.Context,
	runner sandbox.Runner,
	root *file.Root,
	policy sandbox.Policy,
	args Args,
) (string, tool.ToolCallMetrics, error) {
	result, err := runner.Run(ctx, root.Name(), args.Command, policy)
	metrics := tool.ToolCallMetrics{
		Kind:       tool.MetricResources,
		CPUTime:    result.CPUTime,
		PeakMemory: result.PeakMemory,
	}
	if err != nil {
		return measured(unfinished(result.Output, err), &metrics), metrics, err
	}

	reportText := measured(report(result, policy), &metrics)

	if result.ExitCode != 0 {
		return reportText, metrics, ErrCommandFailed
	}

	return reportText, metrics, nil
}

var ErrCommandFailed = errors.New("the command failed")

func measured(reportText string, metrics *tool.ToolCallMetrics) string {
	metrics.Lines = int64(len(strutil.Lines(reportText)))
	metrics.Bytes = int64(len(reportText))
	metrics.TotalBytes = metrics.Bytes

	return reportText
}

func unfinished(output string, err error) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}

	return strings.TrimRight(output, "\n") + "\nnote: " + err.Error() + "."
}

func report(result sandbox.Result, policy sandbox.Policy) string {
	output := result.Output

	if result.ExitCode == 0 {
		return output
	}

	status := fmt.Sprintf("exit(%d)", result.ExitCode)
	if output != "" {
		status += ":"
	}

	parts := []string{status}
	if output != "" {
		parts = append(parts, output)
	}

	overrunNote := sandbox.OverrunNotice(result, policy)

	switch killedNote := sandbox.KillNotice(result, policy); {
	case killedNote != "":
		parts = append(parts, killedNote)
	case policy.Yolo:
	case matches(output, denials):
		parts = append(parts, note(policy))
	case overrunNote != "":
		parts = append(parts, overrunNote)
	}

	return strings.Join(parts, "\n")
}

var denials = []string{
	"Permission denied",
	"Operation not permitted",
	"Address family not supported",
}

func matches(output string, wordings []string) bool {
	for _, wording := range wordings {
		if strings.Contains(output, wording) {
			return true
		}
	}

	return false
}

func note(policy sandbox.Policy) string {
	reach := "external networks and the host's loopback interface are unavailable."
	if policy.Network {
		reach = "this command ran on the host network."
	}

	lines := []string{
		"note: this command ran in a sandbox.",
		reach,
		"writable: " + strings.Join(policy.Write, ", "),
	}

	if len(policy.Read) > 0 {
		lines = append(lines, "readable: "+strings.Join(policy.Read, ", "))
	}

	return strings.Join(lines, "\n")
}
