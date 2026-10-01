package migrate

import (
	"strings"
)

const (
	legacyToolTable = "ui.theme.tool"
	defaultSlotKey  = "default"
)

type themeSlot struct {
	tool   string
	action string
}

var legacyToolKinds = map[string]themeSlot{
	"bash_host_network": {tool: "bash", action: "host_network"},
	"job_start":         {tool: "job", action: "start"},
	"job_stop":          {tool: "job", action: "stop"},
	"job_restart":       {tool: "job", action: "restart"},
	"job_discard":       {tool: "job", action: "discard"},
	"job_prune":         {tool: "job", action: "prune"},
	"job_status":        {tool: "job", action: "status"},
	"job_output":        {tool: "job", action: "output"},
	"job_wait_any":      {tool: "job", action: "wait_any"},
	"job_wait_all":      {tool: "job", action: "wait_all"},
	"job_list":          {tool: "job", action: "list"},
	"expose":            {tool: "forward", action: ""},
	"expose_add":        {tool: "forward", action: "add"},
	"expose_remove":     {tool: "forward", action: "remove"},
	"expose_list":       {tool: "forward", action: "list"},
}

func themeSlotOf(kind string) string {
	tool, action := kind, ""
	if slot, isKnown := legacyToolKinds[kind]; isKnown {
		tool, action = slot.tool, slot.action
	}
	if action == "" {
		action = defaultSlotKey
	}

	return tool + "." + action
}

func nestToolThemeKinds(data []byte) []byte {
	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	isInsideToolTable := false

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[") {
			header := strings.Trim(trimmedLine, "[] \t")
			isInsideToolTable = header == legacyToolTable
			if headerKind, isKind := strings.CutPrefix(header, legacyToolTable+"."); isKind {
				kind, rest, hasRest := strings.Cut(headerKind, ".")
				slot := themeSlotOf(kind)
				if hasRest {
					slot += "." + rest
				}
				lines[i] = strings.Replace(line, headerKind, slot, 1)
			}
			continue
		}
		if !isInsideToolTable {
			continue
		}
		if key, hasKey := configLineKey(line); hasKey {
			kind, rest, hasRest := strings.Cut(key, ".")
			slot := themeSlotOf(kind)
			if hasRest {
				slot += "." + rest
			}
			lines[i] = strings.Replace(line, key, slot, 1)
		}
	}

	joinedLines := strings.Join(lines, "\n")
	if hasFinalNewline {
		joinedLines += "\n"
	}

	return []byte(joinedLines)
}
