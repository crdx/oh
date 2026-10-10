package migrate

import (
	"errors"
	"reflect"
	"slices"
	"strings"
)

const defaultsTable = "defaults"

type formatThirteenSetting struct {
	table string
	key   string
}

type relocation struct {
	from formatThirteenSetting
	to   string
}

var formatThirteenDefaults = []relocation{
	{from: formatThirteenSetting{table: "caps", key: "default"}, to: "caps"},
	{from: formatThirteenSetting{table: "model", key: "effort"}, to: "effort"},
	{from: formatThirteenSetting{table: "model", key: "fast"}, to: "fast"},
	{from: formatThirteenSetting{table: "tool", key: "output"}, to: "tool_output"},
}

var formatThirteenSentinels = []formatThirteenSetting{
	{table: "ui", key: "currency"},
	{table: "ports", key: "hostname"},
	{table: "editor", key: "command"},
}

var errDefaultsNotMoved = errors.New(
	"the defaults could not be moved; move caps.default, model.effort, model.fast and tool.output " +
		"into [defaults] as caps, effort, fast and tool_output by hand, and delete an empty ui.currency, " +
		"ports.hostname or editor.command",
)

type locatedRelocation struct {
	relocation
	settingLocation

	writtenLines []string
}

func standardiseDefaults(data []byte, document map[string]any) ([]byte, error) {
	relocations := requestedRelocations(document)
	if len(relocations) == 0 {
		return data, nil
	}
	if _, hasDefaults := document[defaultsTable]; hasDefaults {
		return nil, errDefaultsNotMoved
	}

	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	settings := make([]locatedRelocation, 0, len(relocations))
	for _, request := range relocations {
		location, isFound := locateSetting(lines, request.from.table, request.from.key)
		if !isFound {
			return nil, errDefaultsNotMoved
		}
		settings = append(settings, locatedRelocation{
			relocation: request, settingLocation: location, writtenLines: lines[location.start:location.end],
		})
	}

	migratedText := strings.Join(relocateLines(lines, settings, emptyHeaders(document, settings)), "\n")
	if hasFinalNewline {
		migratedText += "\n"
	}

	if !areDefaultsMoved(document, migratedText, relocations) {
		return nil, errDefaultsNotMoved
	}

	return []byte(migratedText), nil
}

func requestedRelocations(document map[string]any) []relocation {
	var relocations []relocation
	for _, candidate := range formatThirteenDefaults {
		if _, isSet := settingValue(document, candidate.from); isSet {
			relocations = append(relocations, candidate)
		}
	}
	for _, sentinel := range formatThirteenSentinels {
		if value, isSet := settingValue(document, sentinel); isSet && isEmptySentinel(value) {
			relocations = append(relocations, relocation{from: sentinel})
		}
	}

	return relocations
}

func settingValue(document map[string]any, setting formatThirteenSetting) (any, bool) {
	table, _ := document[setting.table].(map[string]any)
	value, isSet := table[setting.key]

	return value, isSet
}

func isEmptySentinel(value any) bool {
	switch typedValue := value.(type) {
	case string:
		return strings.TrimSpace(typedValue) == ""
	case []any:
		return len(typedValue) == 0
	default:
		return false
	}
}

func emptyHeaders(document map[string]any, settings []locatedRelocation) map[int]bool {
	removedKeys := map[string][]string{}
	headers := map[string]int{}
	for _, setting := range settings {
		removedKeys[setting.from.table] = append(removedKeys[setting.from.table], setting.from.key)
		if !setting.isDotted {
			headers[setting.from.table] = setting.table
		}
	}

	headersToRemove := map[int]bool{}
	for table, keys := range removedKeys {
		writtenTable, _ := document[table].(map[string]any)
		isEmptied := true
		for key := range writtenTable {
			if !slices.Contains(keys, key) {
				isEmptied = false
				break
			}
		}
		if header, hasHeader := headers[table]; isEmptied && hasHeader {
			headersToRemove[header] = true
		}
	}

	return headersToRemove
}

func relocateLines(lines []string, settings []locatedRelocation, headersToRemove map[int]bool) []string {
	isRemoved := make([]bool, len(lines))
	isTouched := make([]bool, len(lines)+1)
	for header := range headersToRemove {
		isRemoved[header] = true
		isTouched[header+1] = true
	}
	for _, setting := range settings {
		for i := setting.start; i < setting.end; i++ {
			isRemoved[i] = true
		}
		isTouched[setting.end] = true
	}

	insertAt, block := defaultsBlock(settings)
	if len(block) > 0 && block[len(block)-1] == "" && !isHeaderNext(lines, isRemoved, insertAt) {
		block = block[:len(block)-1]
	}

	var migratedLines []string
	for i, line := range lines {
		if i == insertAt {
			migratedLines = append(migratedLines, block...)
		}
		if isRemoved[i] {
			continue
		}
		isBlank := strings.TrimSpace(line) == ""
		isAfterBlank := len(migratedLines) == 0 || strings.TrimSpace(migratedLines[len(migratedLines)-1]) == ""
		if isBlank && isTouched[i] && isAfterBlank {
			continue
		}
		migratedLines = append(migratedLines, line)
	}
	if insertAt >= len(lines) {
		migratedLines = append(migratedLines, block...)
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		for len(migratedLines) > 0 && strings.TrimSpace(migratedLines[len(migratedLines)-1]) == "" {
			migratedLines = migratedLines[:len(migratedLines)-1]
		}
	}

	return migratedLines
}

func isHeaderNext(lines []string, isRemoved []bool, from int) bool {
	for i := max(from, 0); i < len(lines); i++ {
		if !isRemoved[i] {
			return strings.HasPrefix(strings.TrimSpace(lines[i]), "[")
		}
	}

	return false
}

func defaultsBlock(settings []locatedRelocation) (int, []string) {
	var relocations []locatedRelocation
	isDotted := false
	for _, setting := range settings {
		if setting.to == "" {
			continue
		}
		relocations = append(relocations, setting)
		isDotted = isDotted || setting.isDotted
	}
	if len(relocations) == 0 {
		return -1, nil
	}

	insertAt := -1
	prefix := ""
	var block []string
	if isDotted {
		prefix = defaultsTable + "."
		for _, setting := range relocations {
			if setting.isDotted && (insertAt < 0 || setting.start < insertAt) {
				insertAt = setting.start
			}
		}
	} else {
		block = append(block, "["+defaultsTable+"]")
		for _, setting := range relocations {
			if insertAt < 0 || setting.table < insertAt {
				insertAt = setting.table
			}
		}
	}

	for _, setting := range relocations {
		block = append(block, rekeyedLines(setting.writtenLines, prefix+setting.to)...)
	}
	if !isDotted {
		block = append(block, "")
	}

	return insertAt, block
}

func rekeyedLines(writtenLines []string, key string) []string {
	first := strings.TrimLeft(writtenLines[0], " \t")

	return append([]string{rewriteLineKey(first, key)}, writtenLines[1:]...)
}

func areDefaultsMoved(document map[string]any, migratedText string, relocations []relocation) bool {
	_, migratedDocument, err := readConfigDocument([]byte(migratedText))
	if err != nil {
		return false
	}
	delete(migratedDocument, "version")

	want := cloneDocument(document)
	delete(want, "version")
	defaults := map[string]any{}
	for _, request := range relocations {
		table, _ := want[request.from.table].(map[string]any)
		if request.to != "" {
			defaults[request.to] = table[request.from.key]
		}
		delete(table, request.from.key)
		if len(table) == 0 {
			delete(want, request.from.table)
		}
	}
	if len(defaults) > 0 {
		want[defaultsTable] = defaults
	}

	return reflect.DeepEqual(want, migratedDocument)
}
