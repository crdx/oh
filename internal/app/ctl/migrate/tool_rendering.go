package migrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func completeToolCallRendering(line map[string]json.RawMessage) error {
	return with(line, func(event map[string]json.RawMessage) error {
		if string(event["kind"]) != `"tool_call_request"` {
			return nil
		}

		name, isNamed := historicalString(event["name"])
		if !isNamed {
			return nil
		}
		arguments := historicalArguments(event["arguments"])

		switch name {
		case "bash":
			event["show_output"] = json.RawMessage("true")
		case "fetch":
			if format, isPresent := historicalArgument(arguments, "type"); isPresent {
				setHistoricalString(event, "detail", "as "+format)
			}
		case "read":
			if err := completeHistoricalReadRendering(event); err != nil {
				return err
			}
		case "job":
			completeHistoricalJobRendering(event, arguments)
		case "expose":
			completeHistoricalExposeRendering(event, arguments)
		case "notify":
			if detail, isPresent := historicalString(event["detail"]); isPresent {
				setHistoricalString(event, "detail", "— "+detail)
			}
		}

		return nil
	})
}

func completeHistoricalReadRendering(event map[string]json.RawMessage) error {
	detail, hasDetail := historicalString(event["detail"])
	if hasDetail && isHistoricalLineRange(detail) {
		setHistoricalString(event, "path_line", detail)
	}

	subject, hasSubject := historicalString(event["render"])
	if !hasSubject || filepath.Base(subject) != "SKILL.md" {
		return nil
	}
	directory := filepath.Dir(subject)
	if filepath.Base(filepath.Dir(directory)) != "skills" {
		return nil
	}

	skillName := filepath.Base(directory)
	setHistoricalString(event, "rendering_kind", "skill")
	emphasis, err := json.Marshal(map[string]string{"kind": "focus", "value": skillName})
	if err != nil {
		return err
	}
	event["emphasis"] = emphasis
	return nil
}

func completeHistoricalJobRendering(event map[string]json.RawMessage, arguments map[string]json.RawMessage) {
	action, _ := historicalArgument(arguments, "action")
	name, _ := historicalArgument(arguments, "name")
	command, hasCommand := historicalArgument(arguments, "command")

	switch action {
	case "start":
		if hasCommand && strings.TrimSpace(command) != "" {
			setHistoricalString(event, "rendering_kind", "job_start")
			completeHistoricalShellContinuations(event)
		} else {
			setHistoricalString(event, "rendering_kind", "job_restart")
		}
		setHistoricalString(event, "render", name)
		delete(event, "detail")
	case "wait":
		waitFor, _ := historicalArgument(arguments, "wait_for")
		if waitFor == "" {
			waitFor = "any"
		}
		separator := " || "
		if waitFor == "all" {
			separator = " && "
		}
		setHistoricalString(event, "rendering_kind", "job_wait_"+waitFor)
		setHistoricalString(event, "render", strings.Join(historicalJobNames(arguments), separator))
		emphasis, err := json.Marshal(map[string]string{"kind": "syntax", "value": "bash"})
		if err == nil {
			event["emphasis"] = emphasis
		}
		if seconds, isPresent := historicalIntegerArgument(arguments, "wait_seconds"); isPresent && seconds > 0 {
			setHistoricalString(event, "detail", fmt.Sprintf("up to %ds", seconds))
		} else {
			delete(event, "detail")
		}
	case "list":
		setHistoricalString(event, "rendering_kind", "job_list")
		setHistoricalString(event, "render", "jobs")
		delete(event, "detail")
	case "prune":
		setHistoricalString(event, "rendering_kind", "job_prune")
		setHistoricalString(event, "render", "jobs")
		delete(event, "detail")
	case "status", "output", "stop", "discard":
		setHistoricalString(event, "rendering_kind", "job_"+action)
		setHistoricalString(event, "render", name)
		delete(event, "detail")
	}
}

func completeHistoricalShellContinuations(event map[string]json.RawMessage) {
	var continuations []map[string]json.RawMessage
	if json.Unmarshal(event["continuation"], &continuations) != nil {
		return
	}
	for _, continuation := range continuations {
		setHistoricalString(continuation, "kind", "bash")
		delete(continuation, "name")
		delete(continuation, "role")
		continuation["show_output"] = json.RawMessage("true")
	}
	encodedContinuations, err := json.Marshal(continuations)
	if err == nil {
		event["continuation"] = encodedContinuations
	}
}

func completeHistoricalExposeRendering(event map[string]json.RawMessage, arguments map[string]json.RawMessage) {
	action, _ := historicalArgument(arguments, "action")
	port, _ := historicalIntegerArgument(arguments, "port")
	switch action {
	case "add":
		setHistoricalString(event, "rendering_kind", "expose_add")
		subject := strconv.Itoa(port)
		if jobName, isPresent := historicalArgument(arguments, "job_name"); isPresent && jobName != "" {
			subject = jobName + ":" + subject
		}
		setHistoricalString(event, "render", subject)
		delete(event, "detail")
	case "remove":
		setHistoricalString(event, "rendering_kind", "expose_remove")
		setHistoricalString(event, "render", strconv.Itoa(port))
		delete(event, "detail")
	case "list":
		setHistoricalString(event, "rendering_kind", "expose_list")
		setHistoricalString(event, "render", "exposed ports")
		delete(event, "detail")
	}
}

func historicalArguments(raw json.RawMessage) map[string]json.RawMessage {
	var encodedArguments string
	if json.Unmarshal(raw, &encodedArguments) != nil {
		return nil
	}

	var arguments map[string]json.RawMessage
	_ = json.Unmarshal([]byte(encodedArguments), &arguments)
	return arguments
}

func historicalArgument(arguments map[string]json.RawMessage, name string) (string, bool) {
	return historicalString(arguments[name])
}

func historicalJobNames(arguments map[string]json.RawMessage) []string {
	var names []string
	if json.Unmarshal(arguments["names"], &names) == nil && len(names) > 0 {
		return names
	}
	if name, isPresent := historicalArgument(arguments, "name"); isPresent {
		return []string{name}
	}
	return nil
}

func historicalIntegerArgument(arguments map[string]json.RawMessage, name string) (int, bool) {
	var value int
	if json.Unmarshal(arguments[name], &value) != nil {
		return 0, false
	}
	return value, true
}

func historicalString(raw json.RawMessage) (string, bool) {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func setHistoricalString(values map[string]json.RawMessage, name string, value string) {
	encodedValue, err := json.Marshal(value)
	if err == nil {
		values[name] = encodedValue
	}
}

func isHistoricalLineRange(value string) bool {
	separator := strings.IndexAny(value, "-+")
	if separator < 1 {
		return false
	}
	if _, err := strconv.ParseUint(value[:separator], 10, 64); err != nil {
		return false
	}
	if value[separator] == '+' {
		return separator == len(value)-1
	}
	_, err := strconv.ParseUint(value[separator+1:], 10, 64)
	return err == nil
}
