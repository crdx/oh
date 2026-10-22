package migrate

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"strings"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/toolset"
)

const (
	oldGrantedKey = "enabled"
	grantedKey    = "granted"
)

var (
	oldToolHeader = regexp.MustCompile(`^([\t ]*\[\[?)[\t ]*(tools|"tools"|'tools')(\]|\.)`)
	oldToolKey    = regexp.MustCompile(`^([\t ]*)(tools|"tools"|'tools')([\t ]*(?:\.|=))`)
	enabledKey    = regexp.MustCompile(`^([\t ]*)(enabled|"enabled"|'enabled')([\t ]*=)`)
)

func migrateConfigFromVersionFourteen(data []byte) ([]byte, error) {
	migratedData, err := moveToolsToTool(data)
	if err != nil {
		return nil, err
	}
	return rewriteConfigVersion(migratedData, config.ToolTableFormat), nil
}

func moveToolsToTool(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}
	oldTools, isDefined := document["tools"]
	if !isDefined {
		return data, nil
	}
	if _, isDefined := document["tool"]; isDefined {
		return nil, errors.New("both tool and tools are defined; move the tools entries into tool by hand")
	}
	toolsTable, isTable := oldTools.(map[string]any)
	if !isTable {
		return nil, errors.New("tools is not a table")
	}
	for name, entry := range toolsTable {
		if values, isEntry := entry.(map[string]any); isEntry {
			if _, hasCommand := values["command"]; hasCommand && toolset.IsBuiltin(name) {
				return nil, fmt.Errorf("custom tool %q now names a built-in; rename it before migrating", name)
			}
		}
	}

	migratedData := renameToolSyntax(data)
	_, migratedDocument, err := readConfigDocument(migratedData)
	expectedDocument := maps.Clone(document)
	delete(expectedDocument, "tools")
	for _, entry := range toolsTable {
		if values, isEntry := entry.(map[string]any); isEntry {
			if oldEnabled, isPresent := values["enabled"]; isPresent {
				delete(values, "enabled")
				values[grantedKey] = oldEnabled
			}
		}
	}
	expectedDocument["tool"] = oldTools
	if err != nil || !reflect.DeepEqual(expectedDocument, migratedDocument) {
		return nil, errors.New("tools could not be renamed safely; move its entries into tool by hand")
	}

	return migratedData, nil
}

func renameToolSyntax(data []byte) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	currentTable := ""
	var multilineQuote byte
	for index, line := range lines {
		if multilineQuote != 0 {
			multilineQuote = endingMultilineQuote(line, multilineQuote)
			continue
		}
		line, currentTable = rewriteToolLine(line, currentTable)
		lines[index] = line
		multilineQuote = endingMultilineQuote(line, 0)
	}
	return []byte(strings.Join(lines, ""))
}

func rewriteToolLine(line string, currentTable string) (string, string) {
	if strings.HasPrefix(strings.TrimSpace(line), "[") {
		if match := oldToolHeader.FindStringSubmatchIndex(line); match != nil {
			line = replaceRootToken(line, match[4], match[5])
		}
		return line, tableName(strings.TrimSpace(line))
	}
	key, hasKey := configLineKey(line)
	if !hasKey {
		return line, currentTable
	}
	switch {
	case currentTable == "":
		return rewriteRootToolLine(line, key), currentTable
	case isToolEntryTable(currentTable):
		return renameLeadingAccess(line), currentTable
	case isToolRootTable(currentTable):
		return renameInlineAccess(line, 1), currentTable
	default:
		return line, currentTable
	}
}

func rewriteRootToolLine(line string, key string) string {
	if match := oldToolKey.FindStringSubmatchIndex(line); match != nil {
		line = replaceRootToken(line, match[4], match[5])
		key, _ = configLineKey(line)
	}
	switch {
	case isToolRootKey(key):
		return renameInlineAccess(line, 2)
	case isToolDottedKey(key):
		return renameDottedAccess(line)
	default:
		return line
	}
}

func replaceRootToken(line string, start int, end int) string {
	return line[:start] + strings.Replace(line[start:end], "tools", "tool", 1) + line[end:]
}

func isToolRootKey(key string) bool {
	name := strings.TrimSpace(key)
	return name == "tool" || name == `"tool"` || name == "'tool'"
}

func isToolDottedKey(key string) bool {
	name := strings.TrimSpace(key)
	return strings.HasPrefix(name, "tool.") || strings.HasPrefix(name, `"tool".`) || strings.HasPrefix(name, "'tool'.")
}

func isToolRootTable(name string) bool { return isToolRootKey(name) }

func isToolEntryTable(name string) bool { return isToolDottedKey(name) }

func renameLeadingAccess(line string) string {
	match := enabledKey.FindStringSubmatchIndex(line)
	if match == nil {
		return line
	}
	return line[:match[4]] + strings.Replace(line[match[4]:match[5]], oldGrantedKey, grantedKey, 1) + line[match[5]:]
}

func renameDottedAccess(line string) string {
	key, isSet := configLineKey(line)
	if !isSet {
		return line
	}
	name := strings.TrimRight(key, " \t")
	for _, suffix := range []string{"enabled", `"enabled"`, "'enabled'"} {
		if strings.HasSuffix(name, "."+suffix) {
			return line[:len(name)-len(suffix)] + strings.Replace(suffix, oldGrantedKey, grantedKey, 1) + line[len(name):]
		}
	}
	return line
}

func renameInlineAccess(line string, wantedDepth int) string {
	key, isSet := configLineKey(line)
	if !isSet {
		return line
	}
	start := len(key) + 1
	depth := 0
	var quote byte
	isEscaped := false
	for index := start; index < len(line); index++ {
		character := line[index]
		if quote != 0 {
			if character == '\\' && quote == '"' && !isEscaped {
				isEscaped = true
				continue
			}
			if character == quote && !isEscaped {
				quote = 0
			}
			isEscaped = false
			continue
		}
		if depth == wantedDepth && (character == 'e' || character == '"' || character == '\'') &&
			(index == 0 || strings.ContainsRune("{, \t", rune(line[index-1]))) {
			wasReplaced := false
			for _, keyToken := range []string{"enabled", `"enabled"`, "'enabled'"} {
				if !strings.HasPrefix(line[index:], keyToken) ||
					!strings.HasPrefix(strings.TrimLeft(line[index+len(keyToken):], " \t"), "=") {
					continue
				}
				line = line[:index] + strings.Replace(keyToken, oldGrantedKey, grantedKey, 1) + line[index+len(keyToken):]
				index += len(keyToken) + len(grantedKey) - len(oldGrantedKey) - 1
				wasReplaced = true
				break
			}
			if wasReplaced {
				continue
			}
		}
		switch character {
		case '"', '\'':
			quote = character
		case '#':
			return line
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return line
}

func endingMultilineQuote(line string, initial byte) byte {
	quote := initial
	for index := 0; index < len(line); index++ {
		character := line[index]
		if quote != 0 {
			if quote == '"' && character == '\\' {
				index++
				continue
			}
			if character == quote && strings.HasPrefix(line[index:], strings.Repeat(string(quote), 3)) {
				quote = 0
				index += 2
			}
			continue
		}
		if character == '#' {
			return 0
		}
		if character != '"' && character != '\'' {
			continue
		}
		if strings.HasPrefix(line[index:], strings.Repeat(string(character), 3)) {
			quote = character
			index += 2
			continue
		}
		index = skipQuoted(line, index, character)
	}
	return quote
}

func skipQuoted(line string, start int, quote byte) int {
	for index := start + 1; index < len(line); index++ {
		if quote == '"' && line[index] == '\\' {
			index++
			continue
		}
		if line[index] == quote {
			return index
		}
	}
	return len(line)
}
