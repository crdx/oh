package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/format"
)

type ConfigOptions struct {
	Path   string
	DryRun bool
}

func MigrateConfig(options ConfigOptions) (int, bool, error) {
	data, err := os.ReadFile(options.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return config.Format, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	fromFormat, _, err := readConfigDocument(data)
	if err != nil {
		return 0, true, err
	}
	if err := format.Check(fromFormat, config.Format); err != nil {
		return fromFormat, true, fmt.Errorf("%w: upgrade oh", err)
	}
	if fromFormat == config.Format {
		return fromFormat, true, nil
	}

	migratedData := data
	for format := fromFormat; format < config.Format; format++ {
		migrationStep, isFound := configSteps[format]
		if !isFound {
			return fromFormat, true, fmt.Errorf("nothing knows how to migrate config format %d", format)
		}
		migratedData, err = migrationStep(migratedData)
		if err != nil {
			return fromFormat, true, fmt.Errorf("format %d: %w", format, err)
		}
	}
	if options.DryRun {
		return fromFormat, true, nil
	}

	backupPath := configBackupPath(options.Path)
	if err := keepConfigCopy(backupPath, data); err != nil {
		return fromFormat, true, err
	}
	if err := writeConfig(options.Path, migratedData); err != nil {
		return fromFormat, true, err
	}

	return fromFormat, true, nil
}

type configStep func(data []byte) ([]byte, error)

var configSteps = map[int]configStep{
	config.InitialFormat:           migrateConfigFromVersionOne,
	config.RoundRobinFormat:        migrateConfigFromVersionTwo,
	config.SegmentNamesFormat:      migrateConfigFromVersionThree,
	config.EditorCommandFormat:     migrateConfigFromVersionFour,
	config.SnippetDefinitionFormat: migrateConfigFromVersionFive,
	config.RetiredTpsFormat:        migrateConfigFromVersionSix,
	config.TurnTimerFormat:         migrateConfigFromVersionSeven,
	config.OllamaHostFormat:        migrateConfigFromVersionEight,
	config.ContinueMessageFormat:   migrateConfigFromVersionNine,
}

func migrateConfigFromVersionNine(data []byte) ([]byte, error) {
	if _, _, err := readConfigDocument(data); err != nil {
		return nil, err
	}

	migratedData := renameTableKey(data, "ui", "stream", "streaming")

	return rewriteConfigVersion(migratedData, config.StreamingNameFormat), nil
}

func renameTableKey(data []byte, table string, oldKey string, newKey string) []byte {
	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	isInsideTable := false

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[") {
			isInsideTable = trimmedLine == "["+table+"]"
			continue
		}
		if !isInsideTable {
			continue
		}
		if found, hasKey := configLineKey(line); hasKey && found == oldKey {
			lines[i] = rewriteLineKey(line, newKey)
			break
		}
	}

	joinedLines := strings.Join(lines, "\n")
	if hasFinalNewline {
		joinedLines += "\n"
	}

	return []byte(joinedLines)
}

func migrateConfigFromVersionEight(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}

	migratedData := data
	if _, hasMessage := document["get_on_with_it_message"]; hasMessage {
		migratedData = moveRootKeyIntoTable(migratedData, "get_on_with_it_message", "input", "continue")
	}

	return rewriteConfigVersion(migratedData, config.ContinueMessageFormat), nil
}

func migrateConfigFromVersionSeven(data []byte) ([]byte, error) {
	if _, _, err := readConfigDocument(data); err != nil {
		return nil, err
	}

	return rewriteConfigVersion(data, config.OllamaHostFormat), nil
}

func migrateConfigFromVersionSix(data []byte) ([]byte, error) {
	if _, _, err := readConfigDocument(data); err != nil {
		return nil, err
	}

	migratedData := renameConfigSegment(data, "turn-elapsed", "turn-timer")
	migratedData = renameConfigSegment(migratedData, "working-directory", "workspace-dir")

	return rewriteConfigVersion(migratedData, config.TurnTimerFormat), nil
}

func migrateConfigFromVersionFive(data []byte) ([]byte, error) {
	if _, _, err := readConfigDocument(data); err != nil {
		return nil, err
	}

	migratedData := removeConfigSegment(data, "last-tps")

	return rewriteConfigVersion(migratedData, config.RetiredTpsFormat), nil
}

func migrateConfigFromVersionFour(data []byte) ([]byte, error) {
	return rewriteConfigVersion(data, config.SnippetDefinitionFormat), nil
}

func migrateConfigFromVersionThree(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}

	migratedData := data
	if _, hasEditor := document["editor"]; hasEditor {
		migratedData = moveRootKeyIntoTable(migratedData, "editor", "editor", "command")
	}

	return rewriteConfigVersion(migratedData, config.EditorCommandFormat), nil
}

func moveRootKeyIntoTable(data []byte, name string, table string, key string) []byte {
	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	tableStart := len(lines)
	movedAt := -1
	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "[") {
			tableStart = i
			break
		}

		if movedAt < 0 {
			if found, hasKey := configLineKey(line); hasKey && found == name {
				movedAt = i
			}
		}
	}

	if movedAt < 0 {
		return data
	}

	migratedLines := append([]string(nil), lines[:movedAt]...)
	migratedLines = append(migratedLines, lines[movedAt+1:tableStart]...)
	migratedLines = appendWithoutTrailingBlank(migratedLines, "", "["+table+"]", rewriteLineKey(lines[movedAt], key))
	if tableStart < len(lines) {
		migratedLines = append(migratedLines, "")
		migratedLines = append(migratedLines, lines[tableStart:]...)
	}

	joinedLines := strings.Join(migratedLines, "\n")
	if hasFinalNewline {
		joinedLines += "\n"
	}

	return []byte(joinedLines)
}

func rewriteLineKey(line string, key string) string {
	writtenKey, value, found := strings.Cut(line, "=")
	if !found {
		return line
	}

	gap := writtenKey[len(strings.TrimRight(writtenKey, " \t")):]

	return key + gap + "=" + value
}

func migrateConfigFromVersionTwo(data []byte) ([]byte, error) {
	if _, _, err := readConfigDocument(data); err != nil {
		return nil, err
	}

	renamedData := renameConfigSegment(data, "current-session", "session-name")
	renamedData = renameConfigSegment(renamedData, "current-time", "local-time")

	return rewriteConfigVersion(renamedData, config.SegmentNamesFormat), nil
}

func renameConfigSegment(data []byte, oldName string, newName string) []byte {
	pattern := regexp.MustCompile(`\bsegment\s*=\s*["']` + regexp.QuoteMeta(oldName) + `["']`)

	return pattern.ReplaceAllFunc(data, func(match []byte) []byte {
		return bytes.Replace(match, []byte(oldName), []byte(newName), 1)
	})
}

func removeConfigSegment(data []byte, name string) []byte {
	quotedName := regexp.QuoteMeta(name)
	table := `\{[\t ]*segment[\t ]*=[\t ]*(?:"` + quotedName + `"|'` + quotedName + `')[\t ]*\}`

	wholeLine := regexp.MustCompile(`(?m)^[\t ]*` + table + `[\t ]*,?[\t ]*(?:#[^\r\n]*)?\r?\n`)
	migratedData := wholeLine.ReplaceAll(data, nil)
	followedByComma := regexp.MustCompile(table + `[\t ]*,[\t ]*`)
	migratedData = followedByComma.ReplaceAll(migratedData, nil)
	precededByComma := regexp.MustCompile(`,[\t ]*` + table)
	migratedData = precededByComma.ReplaceAll(migratedData, nil)
	standalone := regexp.MustCompile(table)

	return standalone.ReplaceAll(migratedData, nil)
}

func readConfigDocument(data []byte) (int, map[string]any, error) {
	var header struct {
		Version int `toml:"version"`
	}
	if _, err := toml.Decode(string(data), &header); err != nil {
		return 0, nil, err
	}

	var document map[string]any
	if _, err := toml.Decode(string(data), &document); err != nil {
		return 0, nil, err
	}

	if header.Version == 0 {
		header.Version = config.InitialFormat
	}

	return header.Version, document, nil
}

func migrateConfigFromVersionOne(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}

	providerName, hasProvider, err := configString(document, "provider")
	if err != nil {
		return nil, err
	}
	model, hasLegacyModel, err := configString(document, "model")
	if err != nil {
		if _, hasModelTable := document["model"].(map[string]any); !hasModelTable {
			return nil, err
		}
		hasLegacyModel = false
	}
	effort, hasEffort, err := configString(document, "effort")
	if err != nil {
		return nil, err
	}

	selection := ""
	if hasLegacyModel && model != "" {
		if !hasProvider {
			providerName = "codex"
		}
		if !hasEffort {
			effort = defaultConfigEffort(providerName)
		}
		if providerName == "" || effort == "" {
			return nil, errors.New("the legacy model needs both a provider and an effort")
		}
		selection = providerName + "/" + model + "@" + effort
	}

	removedKeys := map[string]bool{
		"version":  true,
		"provider": hasProvider,
		"effort":   hasEffort,
		"model":    hasLegacyModel,
	}

	return rewriteVersionOneConfig(data, removedKeys, selection), nil
}

func configString(document map[string]any, name string) (string, bool, error) {
	value, isFound := document[name]
	if !isFound {
		return "", false, nil
	}

	writtenValue, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s is not a string", name)
	}

	return writtenValue, true, nil
}

func defaultConfigEffort(providerName string) string {
	switch providerName {
	case "codex", "anthropic":
		return "high"
	default:
		return ""
	}
}

func rewriteVersionOneConfig(data []byte, removedKeys map[string]bool, selection string) []byte {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	tableStart := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			tableStart = i
			break
		}
	}

	root := make([]string, 0, tableStart+1)
	for _, line := range lines[:tableStart] {
		key, hasKey := configLineKey(line)
		if hasKey && removedKeys[key] {
			continue
		}
		root = append(root, line)
	}

	versionAt := 0
	for versionAt < len(root) {
		trimmedLine := strings.TrimSpace(root[versionAt])
		if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") {
			break
		}
		versionAt++
	}
	prefix := appendWithoutTrailingBlank(append([]string(nil), root[:versionAt]...))
	if len(prefix) > 0 {
		prefix = append(prefix, "")
	}
	prefix = append(prefix, fmt.Sprintf("version = %d", config.RoundRobinFormat))
	root = append(prefix, root[versionAt:]...)

	if selection != "" {
		root = appendWithoutTrailingBlank(root, "", "[model]", "round_robin = ["+strconv.Quote(selection)+"]", "")
	}

	root = append(root, lines[tableStart:]...)

	return []byte(strings.Join(root, "\n") + "\n")
}

func rewriteConfigVersion(data []byte, version int) []byte {
	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}

		key, hasKey := configLineKey(line)
		if !hasKey || key != "version" {
			continue
		}

		indentation := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = fmt.Sprintf("%sversion = %d", indentation, version)
		if _, comment, hasComment := strings.Cut(line, "#"); hasComment {
			lines[i] += " #" + comment
		}
		break
	}

	migratedText := strings.Join(lines, "\n")
	if hasFinalNewline {
		migratedText += "\n"
	}

	return []byte(migratedText)
}

func configLineKey(line string) (string, bool) {
	trimmedLine := strings.TrimSpace(line)
	if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
		return "", false
	}

	key, _, found := strings.Cut(trimmedLine, "=")
	if !found {
		return "", false
	}

	return strings.TrimSpace(key), true
}

func appendWithoutTrailingBlank(lines []string, addedLines ...string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return append(lines, addedLines...)
}

func configBackupPath(path string) string {
	return fmt.Sprintf("%s.pre-v%d", path, config.Format)
}

func keepConfigCopy(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the configured path is ours
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("a copy is already kept in %s: move it aside first", path)
	}
	if err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

func writeConfig(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "config-*.toml")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}

	return os.Rename(temporaryPath, path)
}
