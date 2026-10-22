package migrate

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/config"
)

func migrateConfigFromVersionFifteen(data []byte) ([]byte, error) {
	migratedData, err := singulariseTables(data)
	if err != nil {
		return nil, err
	}
	migratedData = renameConfigSegment(migratedData, "jobs", "tasks")
	return rewriteConfigVersion(migratedData, config.SingularConfigFormat), nil
}

type tableRename struct {
	formerName string
	name       string
}

var tableRenames = []tableRename{
	{formerName: "snippets", name: "snippet"},
	{formerName: "defaults", name: "default"},
	{formerName: "skills", name: "skill"},
	{formerName: "sandbox", name: "grants"},
}

type entryMove struct {
	formerTable string
	formerKey   string
	table       string
	key         string
}

var entryMoves = []entryMove{
	{formerTable: "ports", formerKey: "hostname", table: "experimental", key: "forward_hostname"},
	{formerTable: "permissions", formerKey: "network", table: "tool.bash", key: "host_network"},
	{formerTable: "permissions", formerKey: "fetch", table: "tool.fetch", key: "permission"},
	{formerTable: "permissions", formerKey: "lookup", table: "tool.lookup", key: "permission"},
}

type movedBlock struct {
	table    string
	key      string
	keyIndex int
	lines    []configLine
}

func singulariseTables(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}

	expectedDocument, changes, err := expectSingularTables(document)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return data, nil
	}
	refusal := fmt.Errorf("the config could not be changed safely; by hand, %s", strings.Join(changes, ", "))

	migratedData := data
	for _, rename := range tableRenames {
		if _, isDefined := document[rename.formerName]; isDefined {
			migratedData = renameRootTable(migratedData, rename.formerName, rename.name)
		}
	}
	for _, formerTable := range movedTables() {
		if _, isDefined := document[formerTable]; !isDefined {
			continue
		}
		var isMoved bool
		if migratedData, isMoved = moveEntries(migratedData, document, formerTable); !isMoved {
			return nil, refusal
		}
	}

	_, migratedDocument, err := readConfigDocument(migratedData)
	if err != nil || !reflect.DeepEqual(expectedDocument, migratedDocument) {
		return nil, refusal
	}

	return migratedData, nil
}

func movedTables() []string {
	var tables []string
	for _, move := range entryMoves {
		if !slices.Contains(tables, move.formerTable) {
			tables = append(tables, move.formerTable)
		}
	}
	return tables
}

func movesFrom(formerTable string) []entryMove {
	return slices.DeleteFunc(slices.Clone(entryMoves), func(move entryMove) bool {
		return move.formerTable != formerTable
	})
}

func expectSingularTables(document map[string]any) (map[string]any, []string, error) {
	expectedDocument := cloneDocument(document)
	var changes []string

	for _, rename := range tableRenames {
		value, isDefined := document[rename.formerName]
		if !isDefined {
			continue
		}
		if _, isDefined := document[rename.name]; isDefined {
			return nil, nil, fmt.Errorf(
				"both %s and %s are defined; move the %s entries into %s by hand",
				rename.formerName, rename.name, rename.formerName, rename.name,
			)
		}
		delete(expectedDocument, rename.formerName)
		expectedDocument[rename.name] = value
		changes = append(changes, fmt.Sprintf("rename %s to %s", rename.formerName, rename.name))
	}

	for _, formerTable := range movedTables() {
		formerValue, isDefined := expectedDocument[formerTable]
		if !isDefined {
			continue
		}
		formerEntries, isTable := formerValue.(map[string]any)
		if !isTable {
			return nil, nil, fmt.Errorf("%s is not a table", formerTable)
		}
		for _, move := range movesFrom(formerTable) {
			value, isDefined := formerEntries[move.formerKey]
			if !isDefined {
				continue
			}
			target, err := ensureTable(expectedDocument, move.table)
			if err != nil {
				return nil, nil, err
			}
			if _, isDefined := target[move.key]; isDefined {
				return nil, nil, fmt.Errorf(
					"both %s.%s and %s.%s are defined; remove one by hand",
					move.formerTable, move.formerKey, move.table, move.key,
				)
			}
			target[move.key] = value
			delete(formerEntries, move.formerKey)
			changes = append(changes, fmt.Sprintf(
				"move %s.%s to %s.%s", move.formerTable, move.formerKey, move.table, move.key,
			))
		}
		if len(formerEntries) == 0 {
			delete(expectedDocument, formerTable)
		}
	}

	return expectedDocument, changes, nil
}

func ensureTable(document map[string]any, name string) (map[string]any, error) {
	table := document
	for segment := range strings.SplitSeq(name, ".") {
		value, isDefined := table[segment]
		if !isDefined {
			value = map[string]any{}
			table[segment] = value
		}
		nestedTable, isTable := value.(map[string]any)
		if !isTable {
			return nil, fmt.Errorf("%s is not a table", name)
		}
		table = nestedTable
	}
	return table, nil
}

func isPathDefined(document map[string]any, name string) bool {
	var value any = document
	for segment := range strings.SplitSeq(name, ".") {
		table, isTable := value.(map[string]any)
		if !isTable {
			return false
		}
		var isDefined bool
		if value, isDefined = table[segment]; !isDefined {
			return false
		}
	}
	return true
}

func renameRootTable(data []byte, formerName string, name string) []byte {
	lines := readConfigLines(data)
	header := tableHeaderPattern(formerName)
	rootKey := rootKeyPattern(formerName)
	currentTable := ""
	for index, line := range lines {
		switch {
		case line.isHeader:
			lines[index].text = replaceMatchedToken(line.text, header, formerName, name)
			currentTable = line.headerName
		case currentTable == "" && line.key != "":
			lines[index].text = replaceMatchedToken(line.text, rootKey, formerName, name)
		}
	}

	return joinConfigLines(lines)
}

func tableHeaderPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`^([\t ]*\[\[?[\t ]*)` + tokenPattern(name) + `([\t ]*(?:\]|\.))`)
}

func rootKeyPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`^([\t ]*)` + tokenPattern(name) + `([\t ]*(?:\.|=))`)
}

func tokenPattern(name string) string {
	quotedName := regexp.QuoteMeta(name)
	return `(` + quotedName + `|"` + quotedName + `"|'` + quotedName + `')`
}

func replaceMatchedToken(line string, pattern *regexp.Regexp, formerName string, name string) string {
	match := pattern.FindStringSubmatchIndex(line)
	if match == nil {
		return line
	}

	return line[:match[4]] + strings.Replace(line[match[4]:match[5]], formerName, name, 1) + line[match[5]:]
}

type configLine struct {
	text       string
	isHeader   bool
	isContent  bool
	headerName string
	key        string
}

func readConfigLines(data []byte) []configLine {
	texts := strings.SplitAfter(string(data), "\n")
	if texts[len(texts)-1] == "" {
		texts = texts[:len(texts)-1]
	}

	lines := make([]configLine, len(texts))
	var multilineQuote byte
	for index, text := range texts {
		trimmedText := strings.TrimSpace(text)
		line := configLine{text: text}
		switch {
		case multilineQuote != 0:
			line.isContent = true
		case strings.HasPrefix(trimmedText, "["):
			line.isHeader = true
			line.headerName = unquotedName(tableName(trimmedText))
		default:
			line.isContent = trimmedText != "" && !strings.HasPrefix(trimmedText, "#")
			if key, hasKey := configLineKey(text); hasKey {
				line.key = unquotedName(key)
			}
		}
		multilineQuote = endingMultilineQuote(text, multilineQuote)
		lines[index] = line
	}

	return lines
}

func unquotedName(name string) string {
	return strings.NewReplacer(`"`, "", "'", "", " ", "", "\t", "").Replace(name)
}

func joinConfigLines(lines []configLine) []byte {
	var builder strings.Builder
	for _, line := range lines {
		_, _ = builder.WriteString(line.text)
	}

	return []byte(builder.String())
}

func configTexts(lines []configLine) []string {
	texts := make([]string, len(lines))
	for index, line := range lines {
		texts[index] = strings.TrimRight(line.text, "\r\n")
	}
	return texts
}

func lineEnding(lines []configLine) string {
	for _, line := range lines {
		if strings.HasSuffix(line.text, "\r\n") {
			return "\r\n"
		}
	}
	return "\n"
}

func endLines(lines []configLine, newline string) []configLine {
	for index := range lines {
		if !strings.HasSuffix(lines[index].text, "\n") {
			lines[index].text += newline
		}
	}
	return lines
}

func findTableHeader(lines []configLine, name string) int {
	return slices.IndexFunc(lines, func(line configLine) bool {
		return line.isHeader && line.headerName == name
	})
}

func sectionEnd(lines []configLine, header int) int {
	for index := header + 1; index < len(lines); index++ {
		if lines[index].isHeader {
			return index
		}
	}
	return len(lines)
}

func lastTableContent(lines []configLine, header int) int {
	last := header
	for index := header + 1; index < sectionEnd(lines, header); index++ {
		if lines[index].isContent {
			last = index
		}
	}

	return last
}

func isCommentConfigLine(line configLine) bool {
	return !line.isContent && !line.isHeader && strings.HasPrefix(strings.TrimSpace(line.text), "#")
}

func isBlankConfigLine(line configLine) bool {
	return strings.TrimSpace(line.text) == ""
}

func rewriteIndentedKey(line string, key string) string {
	indentation := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indentation + rewriteLineKey(strings.TrimLeft(line, " \t"), key)
}

func headerComment(header string) string {
	closingBracket := strings.LastIndex(header, "]")
	if closingBracket < 0 {
		return ""
	}
	comment := strings.TrimSpace(header[closingBracket+1:])
	if !strings.HasPrefix(comment, "#") {
		return ""
	}
	return comment
}

func canAddTableHeader(lines []configLine, document map[string]any, table string) bool {
	if isPathDefined(document, table) {
		return false
	}
	root, _, _ := strings.Cut(table, ".")
	if _, isDefined := document[root]; !isDefined {
		return true
	}
	currentTable := ""
	hasHeader := false
	for _, line := range lines {
		if line.isHeader {
			currentTable = line.headerName
			hasHeader = hasHeader || currentTable == root || strings.HasPrefix(currentTable, root+".")
			continue
		}
		if currentTable == "" && (line.key == root || strings.HasPrefix(line.key, root+".")) {
			return false
		}
	}
	return hasHeader
}

func renameDottedEntries(lines []configLine, document map[string]any, moves []entryMove) ([]byte, bool) {
	for _, move := range moves {
		root, _, _ := strings.Cut(move.table, ".")
		if _, isDefined := document[root]; isDefined {
			return nil, false
		}
	}
	for index, line := range lines {
		if line.isHeader {
			break
		}
		for _, move := range moves {
			if line.key == move.formerTable+"."+move.formerKey {
				lines[index].text = rewriteIndentedKey(line.text, move.table+"."+move.key)
			}
		}
	}
	return joinConfigLines(lines), true
}

func cutMovedBlocks(lines []configLine, header int, moves []entryMove) ([]movedBlock, []bool, bool) {
	isRemoved := make([]bool, len(lines))
	end := sectionEnd(lines, header)
	var blocks []movedBlock
	for _, move := range moves {
		start := slices.IndexFunc(lines[header+1:end], func(line configLine) bool {
			return !line.isHeader && line.key == move.formerKey
		})
		if start < 0 {
			continue
		}
		start += header + 1
		settingStop, isComplete := settingEnd(configTexts(lines), start)
		if !isComplete || settingStop > end {
			return nil, nil, false
		}
		first := start
		for first > header+1 && isCommentConfigLine(lines[first-1]) && !isRemoved[first-1] {
			first--
		}
		block := slices.Clone(lines[first:settingStop])
		block[start-first].text = rewriteIndentedKey(block[start-first].text, move.key)
		blocks = append(blocks, movedBlock{table: move.table, key: move.key, keyIndex: start, lines: block})
		for index := first; index < settingStop; index++ {
			isRemoved[index] = true
		}
	}
	return blocks, isRemoved, true
}

func blockTables(blocks []movedBlock) []string {
	var tables []string
	for _, block := range blocks {
		if !slices.Contains(tables, block.table) {
			tables = append(tables, block.table)
		}
	}
	return tables
}

func moveEntries(data []byte, document map[string]any, formerTable string) ([]byte, bool) {
	lines := readConfigLines(data)
	moves := movesFrom(formerTable)
	header := findTableHeader(lines, formerTable)
	if header < 0 {
		return renameDottedEntries(lines, document, moves)
	}

	blocks, isRemoved, isCut := cutMovedBlocks(lines, header, moves)
	if !isCut {
		return nil, false
	}
	end := sectionEnd(lines, header)
	isEmptied := true
	for index := header + 1; index < end; index++ {
		if lines[index].isContent && !isRemoved[index] {
			isEmptied = false
			break
		}
	}
	tables := blockTables(blocks)
	for _, table := range tables {
		if findTableHeader(lines, table) < 0 && !canAddTableHeader(lines, document, table) {
			return nil, false
		}
	}

	if isEmptied && len(tables) == 1 && findTableHeader(lines, tables[0]) < 0 {
		for _, block := range blocks {
			lines[block.keyIndex].text = rewriteIndentedKey(lines[block.keyIndex].text, block.key)
		}
		lines[header].text = replaceMatchedToken(lines[header].text, tableHeaderPattern(formerTable), formerTable, tables[0])
		return joinConfigLines(lines), true
	}

	newline := lineEnding(lines)
	anchor := header
	var headerLines []configLine
	if isEmptied {
		isRemoved[header] = true
		if comment := headerComment(lines[header].text); comment != "" {
			headerLines = append(headerLines, configLine{text: comment + newline})
		}
		if len(tables) == 1 {
			first := header
			for first > 0 && isCommentConfigLine(lines[first-1]) {
				first--
			}
			headerLines = append(slices.Clone(lines[first:header]), headerLines...)
			for index := first; index < header; index++ {
				isRemoved[index] = true
			}
			anchor = first
		}
	}

	keptLines, anchor := removeLines(lines, isRemoved, anchor, isEmptied && (len(tables) == 1 || len(headerLines) == 0))
	if len(tables) == 1 {
		blocks[0].lines = append(headerLines, blocks[0].lines...)
	} else {
		keptLines = slices.Insert(keptLines, anchor, headerLines...)
		anchor += len(headerLines)
	}

	keptLines, createdTables := createTables(keptLines, anchor, blocks, tables, newline)
	for _, table := range tables {
		if slices.Contains(createdTables, table) {
			continue
		}
		var movedLines []configLine
		for _, block := range blocks {
			if block.table == table {
				movedLines = append(movedLines, block.lines...)
			}
		}
		tableHeader := findTableHeader(keptLines, table)
		insertAt := lastTableContent(keptLines, tableHeader) + 1
		endLines(keptLines[insertAt-1:insertAt], newline)
		keptLines = slices.Insert(keptLines, insertAt, endLines(movedLines, newline)...)
	}

	return joinConfigLines(keptLines), true
}

func removeLines(lines []configLine, isRemoved []bool, anchor int, shouldCloseGap bool) ([]configLine, int) {
	var keptLines []configLine
	newAnchor := 0
	for index, line := range lines {
		if index == anchor {
			newAnchor = len(keptLines)
		}
		if !isRemoved[index] {
			keptLines = append(keptLines, line)
		}
	}
	if shouldCloseGap && newAnchor > 0 && isBlankConfigLine(keptLines[newAnchor-1]) &&
		(newAnchor == len(keptLines) || isBlankConfigLine(keptLines[newAnchor])) {
		newAnchor--
		keptLines = slices.Delete(keptLines, newAnchor, newAnchor+1)
	}
	return keptLines, newAnchor
}

func createTables(
	keptLines []configLine, anchor int, blocks []movedBlock, tables []string, newline string,
) ([]configLine, []string) {
	var createdLines []configLine
	var createdTables []string
	for _, table := range tables {
		if findTableHeader(keptLines, table) >= 0 {
			continue
		}
		if len(createdTables) > 0 {
			createdLines = append(createdLines, configLine{text: newline})
		}
		createdTables = append(createdTables, table)
		createdLines = append(createdLines, configLine{text: "[" + table + "]" + newline, isHeader: true, headerName: table})
		for _, block := range blocks {
			if block.table == table {
				createdLines = append(createdLines, block.lines...)
			}
		}
	}
	if len(createdLines) == 0 {
		return keptLines, nil
	}
	createdLines = endLines(createdLines, newline)
	if anchor > 0 && !isBlankConfigLine(keptLines[anchor-1]) {
		endLines(keptLines[anchor-1:anchor], newline)
		createdLines = append([]configLine{{text: newline}}, createdLines...)
	}
	if anchor < len(keptLines) && !isBlankConfigLine(keptLines[anchor]) {
		createdLines = append(createdLines, configLine{text: newline})
	}
	return slices.Insert(keptLines, anchor, createdLines...), createdTables
}
