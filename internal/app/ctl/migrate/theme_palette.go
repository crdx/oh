package migrate

import (
	"errors"
	"maps"
	"reflect"
	"strings"
)

const (
	themeTable       = "ui.theme"
	darkPaletteTable = "ui.theme.dark"
	darkPaletteKey   = "dark"
)

var formatTwelvePaletteKeys = map[string]bool{
	"normal":          true,
	"dim":             true,
	"accent":          true,
	"status_success":  true,
	"status_info":     true,
	"status_warning":  true,
	"status_danger":   true,
	"syntax_type":     true,
	"syntax_literal":  true,
	"syntax_operator": true,
	"syntax_keyword":  true,
	"skill":           true,
	"user":            true,
	"harness":         true,
}

var errPaletteNotMoved = errors.New(
	"the theme colours could not be moved; move every colour under [ui.theme] into [ui.theme.dark] by hand",
)

func moveThemeColoursToDarkPalette(data []byte, document map[string]any) ([]byte, error) {
	if len(paletteKeysOf(document)) == 0 {
		return data, nil
	}

	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	if header, isWholeTable := themeTableOfColoursAlone(lines); isWholeTable {
		lines[header] = strings.Replace(lines[header], themeTable, darkPaletteTable, 1)
	} else {
		lines = prefixThemeColourKeys(lines)
	}

	migratedText := strings.Join(lines, "\n")
	if hasFinalNewline {
		migratedText += "\n"
	}

	if !isPaletteMoved(document, migratedText) {
		return nil, errPaletteNotMoved
	}

	return []byte(migratedText), nil
}

func themeTableOfColoursAlone(lines []string) (int, bool) {
	header := -1
	isInsideTheme := false

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[") {
			isInsideTheme = tableName(trimmedLine) == themeTable
			if isInsideTheme {
				header = i
			}
			continue
		}
		if !isInsideTheme {
			continue
		}
		if key, hasKey := configLineKey(line); hasKey && !formatTwelvePaletteKeys[key] {
			return 0, false
		}
	}

	return header, header >= 0
}

func prefixThemeColourKeys(lines []string) []string {
	table := ""

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[") {
			table = tableName(trimmedLine)
			continue
		}

		key, hasKey := configLineKey(line)
		if !hasKey {
			continue
		}

		var prefix string
		switch table {
		case themeTable:
			prefix = ""
		case "ui":
			prefix = "theme."
		case "":
			prefix = themeTable + "."
		default:
			continue
		}

		colour, isThemeKey := strings.CutPrefix(key, prefix)
		if !isThemeKey || !formatTwelvePaletteKeys[colour] {
			continue
		}

		indentation := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = indentation + rewriteLineKey(strings.TrimLeft(line, " \t"), prefix+darkPaletteKey+"."+colour)
	}

	return lines
}

func tableName(header string) string {
	name, _, _ := strings.Cut(header, "#")

	return strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(name), "[]")), "")
}

func isPaletteMoved(document map[string]any, migratedText string) bool {
	_, migratedDocument, err := readConfigDocument([]byte(migratedText))
	if err != nil {
		return false
	}
	delete(migratedDocument, "version")

	want := cloneDocument(document)
	delete(want, "version")
	theme := themeOf(want)
	dark := map[string]any{}
	for key := range paletteKeysOf(document) {
		dark[key] = theme[key]
		delete(theme, key)
	}
	theme[darkPaletteKey] = dark

	return reflect.DeepEqual(want, migratedDocument)
}

func paletteKeysOf(document map[string]any) map[string]bool {
	found := map[string]bool{}
	for key := range themeOf(document) {
		if formatTwelvePaletteKeys[key] {
			found[key] = true
		}
	}

	return found
}

func themeOf(document map[string]any) map[string]any {
	ui, _ := document["ui"].(map[string]any)
	theme, _ := ui["theme"].(map[string]any)

	return theme
}

func cloneDocument(document map[string]any) map[string]any {
	documentCopy := maps.Clone(document)
	for key, value := range documentCopy {
		if table, isTable := value.(map[string]any); isTable {
			documentCopy[key] = cloneDocument(table)
		}
	}

	return documentCopy
}
