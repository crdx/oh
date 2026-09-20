package toolresult

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"crdx.org/io/internal/app/markdown"
	"crdx.org/io/internal/app/style"
	internaltoolresult "crdx.org/io/internal/toolresult"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/toolbox/bash"
	"crdx.org/io/pkg/toolbox/edit"
	"crdx.org/io/pkg/toolbox/fetch"
	"crdx.org/io/pkg/toolbox/find"
	"crdx.org/io/pkg/toolbox/grep"
	"crdx.org/io/pkg/toolbox/lookup"
	"crdx.org/io/pkg/toolbox/ls"
	"crdx.org/io/pkg/toolbox/notify"
	"crdx.org/io/pkg/toolbox/read"
	"crdx.org/io/pkg/toolbox/title"
	"crdx.org/io/pkg/toolbox/write"
)

const (
	diffContextLines = 3
	partSeparator    = " · "
)

func render(exchange internaltoolresult.Exchange, columns int) string {
	var resultView string
	var err error

	switch exchange.Request.Name {
	case "read":
		resultView, err = renderRead(exchange)
	case "write":
		resultView, err = renderWrite(exchange)
	case "edit":
		resultView, err = renderEdit(exchange)
	case "bash":
		resultView, err = renderBash(exchange)
	case "ls":
		resultView, err = renderList(exchange)
	case "find":
		resultView, err = renderFind(exchange)
	case "grep":
		resultView, err = renderGrep(exchange)
	case "lookup":
		resultView, err = renderLookup(exchange, columns)
	case "fetch":
		resultView, err = renderFetch(exchange, columns)
	case title.Name:
		resultView, err = renderTitle(exchange)
	case "notify":
		resultView, err = renderNotification(exchange)
	default:
		resultView = renderFallback(exchange)
	}
	if err != nil {
		return renderFallback(exchange)
	}
	return resultView
}

func renderRead(exchange internaltoolresult.Exchange) (string, error) {
	var arguments read.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	path := safe(arguments.Path)
	header := fileHeading("read", path, lineRange(arguments.Offset, arguments.Limit))
	body := markdown.HighlightFile(path, successfulText(exchange))
	return withFailure(header+"\n\n"+body, exchange), nil
}

func renderWrite(exchange internaltoolresult.Exchange) (string, error) {
	var arguments write.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	path := safe(arguments.Path)
	header := fileHeading("write", path, util.FormatBytes(len(arguments.Content), 3))
	body := markdown.HighlightFile(path, safe(arguments.Content))
	return withFailure(header+"\n\n"+body, exchange), nil
}

func renderEdit(exchange internaltoolresult.Exchange) (string, error) {
	var arguments edit.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	path := safe(arguments.Path)
	header := fileHeading("edit", path, "")
	body := renderDiff(path, safe(arguments.OldText), safe(arguments.NewText))
	return withFailure(header+"\n\n"+body, exchange), nil
}

func renderBash(exchange internaltoolresult.Exchange) (string, error) {
	var arguments bash.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	command := safe(arguments.Command)
	commandLines := strings.Split(command, "\n")
	for i, line := range commandLines {
		prefix := style.Prompt("$ ")
		if i > 0 {
			prefix = style.Prompt("> ")
		}
		commandLines[i] = prefix + markdown.Emphasise(line, "bash")
	}

	output := safe(exchange.Result.Text)
	switch {
	case output == "":
		output = style.Subtle("(no output)")
	case exchange.Result.Status == agent.ErrorStatus:
		output = style.Failure.Over(output)
	case exchange.Result.Status == agent.CancelledStatus:
		output = style.StoppedTurn.Over(output)
	}
	return strings.Join(commandLines, "\n") + "\n\n" + output, nil
}

func renderList(exchange internaltoolresult.Exchange) (string, error) {
	var arguments ls.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	path := safe(arguments.Path)
	if path == "" {
		path = "."
	}
	body := emphasisePaths(successfulText(exchange))
	return withFailure(heading("ls", path)+"\n\n"+body, exchange), nil
}

func renderFind(exchange internaltoolresult.Exchange) (string, error) {
	var arguments find.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	subject := safe(arguments.Pattern)
	if arguments.Path != "" {
		subject += partSeparator + safe(arguments.Path)
	}
	body := emphasisePaths(successfulText(exchange))
	return withFailure(heading("find", subject)+"\n\n"+body, exchange), nil
}

func renderGrep(exchange internaltoolresult.Exchange) (string, error) {
	var arguments grep.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	subject := markdown.Emphasise(safe(arguments.Pattern), "regexp")
	location := util.JoinNonEmpty(safe(arguments.Path), safe(arguments.Glob))
	if location != "" {
		subject += partSeparator + style.Qualifier(location)
	}
	body := renderGrepMatches(successfulText(exchange))
	return withFailure(style.Heading("grep")+partSeparator+subject+"\n\n"+body, exchange), nil
}

func renderLookup(exchange internaltoolresult.Exchange, columns int) (string, error) {
	var arguments lookup.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	body := renderMarkdown(successfulText(exchange), columns)
	return withFailure(heading("lookup", safe(arguments.Query))+"\n\n"+body, exchange), nil
}

func renderFetch(exchange internaltoolresult.Exchange, columns int) (string, error) {
	var arguments fetch.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	output := successfulText(exchange)
	var body string
	switch arguments.Type {
	case "markdown":
		body = renderMarkdown(output, columns)
	case "clean_html", "raw":
		body = markdown.HighlightText(output, "html")
	case "text":
		body = output
	default:
		body = output
	}
	subject := style.Subject(safe(arguments.URL)) + partSeparator + style.Qualifier(safe(arguments.Type))
	return withFailure(style.Heading("fetch")+partSeparator+subject+"\n\n"+body, exchange), nil
}

func renderTitle(exchange internaltoolresult.Exchange) (string, error) {
	var arguments title.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}
	return withFailure(heading("title", safe(arguments.Title))+"\n\n"+successfulText(exchange), exchange), nil
}

func renderNotification(exchange internaltoolresult.Exchange) (string, error) {
	var arguments notify.Args
	if err := decodeArguments(exchange, &arguments); err != nil {
		return "", err
	}

	header := heading("notify", safe(arguments.Title))
	body := safe(arguments.Message) + "\n\n" + style.Qualifier(safe(arguments.Icon))
	return withFailure(header+"\n\n"+body, exchange), nil
}

func renderFallback(exchange internaltoolresult.Exchange) string {
	header := heading(safe(exchange.Request.Name), "")
	var arguments bytes.Buffer
	if err := json.Indent(&arguments, []byte(exchange.Request.Arguments), "", "    "); err != nil {
		arguments.WriteString(safe(exchange.Request.Arguments))
	}

	parts := []string{header}
	if arguments.Len() > 0 {
		parts = append(parts, markdown.HighlightText(safe(arguments.String()), "json"))
	}
	if exchange.Result.Text != "" {
		parts = append(parts, safe(exchange.Result.Text))
	}
	return strings.Join(parts, "\n\n")
}

func decodeArguments(exchange internaltoolresult.Exchange, destination any) error {
	return json.Unmarshal([]byte(exchange.Request.Arguments), destination)
}

func safe(text string) string {
	return strutil.StripControl(text)
}

func heading(name string, subject string) string {
	if subject == "" {
		return style.Heading(name)
	}
	return style.Heading(name) + partSeparator + style.Subject(subject)
}

func fileHeading(name string, path string, qualifier string) string {
	header := heading(name, path)
	if qualifier != "" {
		header += partSeparator + style.Qualifier(qualifier)
	}
	return header
}

func lineRange(offset int, limit int) string {
	switch {
	case offset > 0 && limit > 0:
		return fmt.Sprintf("lines %d–%d", offset, offset+limit-1)
	case offset > 0:
		return fmt.Sprintf("from line %d", offset)
	case limit > 0:
		return fmt.Sprintf("first %d lines", limit)
	default:
		return ""
	}
}

func successfulText(exchange internaltoolresult.Exchange) string {
	if exchange.Result.Status != agent.SuccessStatus && exchange.Result.Status != agent.InfoStatus {
		return ""
	}
	return safe(exchange.Result.Text)
}

func withFailure(resultView string, exchange internaltoolresult.Exchange) string {
	if exchange.Result.Status == agent.SuccessStatus || exchange.Result.Status == agent.InfoStatus {
		return resultView
	}
	if exchange.Result.Text == "" {
		return resultView
	}
	return resultView + "\n\n" + style.Failure("Error") + "\n" + style.Failure(safe(exchange.Result.Text))
}

func renderMarkdown(text string, columns int) string {
	if columns <= 0 {
		columns = 100
	}
	return strings.Join(markdown.Render(text, columns), "\n")
}

func emphasisePaths(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "(") {
			continue
		}
		lines[i] = style.Subject(line)
	}
	return strings.Join(lines, "\n")
}

func renderGrepMatches(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		path, rest, hasPath := strings.Cut(line, ":")
		lineNumber, match, hasLine := strings.Cut(rest, ":")
		if !hasPath || !hasLine {
			continue
		}
		if _, err := strconv.Atoi(lineNumber); err != nil {
			continue
		}
		prefix := path + ":" + lineNumber + ":"
		lines[i] = style.Address(prefix) + markdown.HighlightFile(filepath.Base(path), match)
	}
	return strings.Join(lines, "\n")
}

func renderDiff(path string, before string, after string) string {
	beforeLines := strutil.Lines(before)
	afterLines := strutil.Lines(after)
	prefixLines := commonPrefix(beforeLines, afterLines)
	suffixLines := commonSuffix(beforeLines[prefixLines:], afterLines[prefixLines:])

	firstLine := max(prefixLines-diffContextLines, 0)
	beforeLast := min(len(beforeLines), len(beforeLines)-suffixLines+diffContextLines)
	afterLast := min(len(afterLines), len(afterLines)-suffixLines+diffContextLines)

	lines := []string{
		"--- " + path,
		"+++ " + path,
		fmt.Sprintf("@@ -%d,%d +%d,%d @@", firstLine+1, beforeLast-firstLine, firstLine+1, afterLast-firstLine),
	}
	for _, line := range beforeLines[firstLine:prefixLines] {
		lines = append(lines, " "+line)
	}
	for _, line := range beforeLines[prefixLines : len(beforeLines)-suffixLines] {
		lines = append(lines, "-"+line)
	}
	for _, line := range afterLines[prefixLines : len(afterLines)-suffixLines] {
		lines = append(lines, "+"+line)
	}
	beforeSuffix := beforeLines[len(beforeLines)-suffixLines : beforeLast]
	for _, line := range beforeSuffix {
		lines = append(lines, " "+line)
	}
	for i, line := range lines {
		lines[i] = markdown.Emphasise(line, "diff")
	}
	return strings.Join(lines, "\n")
}

func commonPrefix(before []string, after []string) int {
	length := min(len(before), len(after))
	for i := range length {
		if before[i] != after[i] {
			return i
		}
	}
	return length
}

func commonSuffix(before []string, after []string) int {
	length := min(len(before), len(after))
	for i := range length {
		if before[len(before)-1-i] != after[len(after)-1-i] {
			return i
		}
	}
	return length
}
