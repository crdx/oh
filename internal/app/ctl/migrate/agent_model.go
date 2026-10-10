package migrate

import (
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	formatThirteenModelTable = "model"
	formatThirteenRotation   = "round_robin"
	agentTable               = "agent"
	agentModelKey            = "model"
)

var errRotationNotMoved = errors.New(
	"model.round_robin could not be moved; move it into [agent] by hand, " +
		"writing a single selection as agent.model",
)

var singleSelectionPattern = regexp.MustCompile(
	`^([\t ]*)[^=]*?([\t ]*)=([\t ]*)\[[\t ]*("(?:[^"\\]|\\.)*"|'[^']*')[\t ]*,?[\t ]*\]([\t ]*(?:#.*)?)$`,
)

type settingLocation struct {
	start    int
	end      int
	isDotted bool
	table    int
}

func moveRotationToAgent(data []byte, document map[string]any) ([]byte, error) {
	modelTable, _ := document[formatThirteenModelTable].(map[string]any)
	rotation, hasRotation := modelTable[formatThirteenRotation]
	if !hasRotation {
		return data, nil
	}
	if _, hasAgent := document[agentTable]; hasAgent {
		return nil, errRotationNotMoved
	}

	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	setting, isFound := locateSetting(lines, formatThirteenModelTable, formatThirteenRotation)
	if !isFound {
		return nil, errRotationNotMoved
	}

	agentLines, agentKey, agentValue := agentSettingLines(lines[setting.start:setting.end], rotation, setting.isDotted)

	var migratedLines []string
	if setting.isDotted {
		migratedLines = append(migratedLines, lines[:setting.start]...)
		migratedLines = append(migratedLines, agentLines...)
		migratedLines = append(migratedLines, lines[setting.end:]...)
	} else {
		migratedLines = moveIntoAgentTable(lines, setting, agentLines)
	}

	migratedText := strings.Join(migratedLines, "\n")
	if hasFinalNewline {
		migratedText += "\n"
	}

	if !isRotationMoved(document, migratedText, agentKey, agentValue) {
		return nil, errRotationNotMoved
	}

	return []byte(migratedText), nil
}

func locateSetting(lines []string, wantedTable string, wantedKey string) (settingLocation, bool) {
	table := ""
	tableHeader := -1

	for i := 0; i < len(lines); i++ {
		trimmedLine := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmedLine, "[") {
			table = tableName(trimmedLine)
			tableHeader = i
			continue
		}

		key, hasKey := configLineKey(lines[i])
		if !hasKey {
			continue
		}
		key = strings.Join(strings.Fields(key), "")

		end, isComplete := settingEnd(lines, i)
		isDotted := table == "" && key == wantedTable+"."+wantedKey
		isInTable := table == wantedTable && key == wantedKey
		if isDotted || isInTable {
			if !isComplete {
				return settingLocation{}, false
			}
			return settingLocation{start: i, end: end, isDotted: isDotted, table: tableHeader}, true
		}
		if isComplete {
			i = end - 1
		}
	}

	return settingLocation{}, false
}

func settingEnd(lines []string, start int) (int, bool) {
	for end := start + 1; end <= len(lines); end++ {
		var value map[string]any
		if _, err := toml.Decode(strings.Join(lines[start:end], "\n"), &value); err == nil {
			return end, true
		}
	}

	return 0, false
}

func agentSettingLines(setting []string, rotation any, isDotted bool) ([]string, string, any) {
	prefix := ""
	if isDotted {
		prefix = agentTable + "."
	}

	if selection, isSingle := singleSelection(rotation); isSingle {
		return singleSelectionLines(setting, prefix+agentModelKey, selection), agentModelKey, selection
	}

	first := setting[0]
	indentation := first[:len(first)-len(strings.TrimLeft(first, " \t"))]
	agentLines := append([]string{indentation + rewriteLineKey(strings.TrimLeft(first, " \t"), prefix+formatThirteenRotation)}, setting[1:]...)

	return agentLines, formatThirteenRotation, rotation
}

func singleSelection(rotation any) (string, bool) {
	selections, isList := rotation.([]any)
	if !isList || len(selections) != 1 {
		return "", false
	}
	selection, isText := selections[0].(string)

	return selection, isText
}

func singleSelectionLines(setting []string, key string, selection string) []string {
	if len(setting) == 1 {
		if match := singleSelectionPattern.FindStringSubmatch(setting[0]); match != nil {
			return []string{match[1] + key + match[2] + "=" + match[3] + match[4] + match[5]}
		}
	}

	indentation := setting[0][:len(setting[0])-len(strings.TrimLeft(setting[0], " \t"))]

	return []string{indentation + key + " = " + strconv.Quote(selection)}
}

func moveIntoAgentTable(lines []string, setting settingLocation, agentLines []string) []string {
	tableEnd := len(lines)
	for i := setting.end; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			tableEnd = i
			break
		}
	}

	modelLines := append(append([]string(nil), lines[setting.table+1:setting.start]...), lines[setting.end:tableEnd]...)
	isModelEmptied := true
	for _, line := range modelLines {
		if strings.TrimSpace(line) != "" {
			isModelEmptied = false
			break
		}
	}

	migratedLines := append([]string(nil), lines[:setting.table]...)
	if isModelEmptied {
		header := strings.Replace(lines[setting.table], formatThirteenModelTable, agentTable, 1)
		migratedLines = append(migratedLines, header)
		migratedLines = append(migratedLines, agentLines...)
		migratedLines = append(migratedLines, lines[setting.end:tableEnd]...)
	} else {
		migratedLines = append(migratedLines, lines[setting.table])
		migratedLines = append(migratedLines, modelLines...)
		migratedLines = appendWithoutTrailingBlank(migratedLines, "", "["+agentTable+"]")
		migratedLines = append(migratedLines, agentLines...)
		if tableEnd < len(lines) {
			migratedLines = append(migratedLines, "")
		}
	}

	return append(migratedLines, lines[tableEnd:]...)
}

func isRotationMoved(document map[string]any, migratedText string, agentKey string, agentValue any) bool {
	_, migratedDocument, err := readConfigDocument([]byte(migratedText))
	if err != nil {
		return false
	}
	delete(migratedDocument, "version")
	dropEmptyModelTable(migratedDocument)

	want := cloneDocument(document)
	delete(want, "version")
	modelTable, _ := want[formatThirteenModelTable].(map[string]any)
	delete(modelTable, formatThirteenRotation)
	dropEmptyModelTable(want)
	want[agentTable] = map[string]any{agentKey: agentValue}

	return reflect.DeepEqual(want, migratedDocument)
}

func dropEmptyModelTable(document map[string]any) {
	if modelTable, isTable := document[formatThirteenModelTable].(map[string]any); isTable && len(modelTable) == 0 {
		delete(document, formatThirteenModelTable)
	}
}
