package migrate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

var legacyContextSourceKinds = map[string]string{
	"harness":              "harness_instructions",
	"harness instructions": "harness_instructions",
}

var legacyCountedContextSources = []*regexp.Regexp{
	regexp.MustCompile(`^skill catalogue \((\d+) skills?\)$`),
	regexp.MustCompile(`^(\d+) skill definitions?$`),
}

const legacySkillDefinitionsKind = "skill_definitions"

var legacyContextSourceKeys = []string{"system_context_sources", "session_context_sources"}

func contextKindsReplaceNames(line map[string]json.RawMessage) error {
	if string(line["kind"]) != `"head"` {
		return nil
	}

	raw, ok := line["meta"]
	if !ok {
		return nil
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("the session meta could not be read: %w", err)
	}

	isChanged := false
	for _, key := range legacyContextSourceKeys {
		rawSources, ok := meta[key]
		if !ok {
			continue
		}

		var sources []map[string]json.RawMessage
		if err := json.Unmarshal(rawSources, &sources); err != nil {
			return fmt.Errorf("the %s could not be read: %w", key, err)
		}

		for _, source := range sources {
			if err := restateContextSource(source); err != nil {
				return err
			}
		}

		restatedSources, err := json.Marshal(sources)
		if err != nil {
			return err
		}
		meta[key] = restatedSources
		isChanged = true
	}

	if !isChanged {
		return nil
	}

	restatedMeta, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	line["meta"] = restatedMeta

	return nil
}

func restateContextSource(source map[string]json.RawMessage) error {
	raw, ok := source["name"]
	if !ok {
		return nil
	}

	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return fmt.Errorf("the context source name could not be read: %w", err)
	}

	kind, count, err := legacyContextSourceKind(name)
	if err != nil {
		return err
	}

	delete(source, "name")
	source["kind"] = json.RawMessage(strconv.Quote(kind))
	if count > 0 {
		source["count"] = json.RawMessage(strconv.Itoa(count))
	}

	return nil
}

func legacyContextSourceKind(name string) (string, int, error) {
	if kind, isKnown := legacyContextSourceKinds[name]; isKnown {
		return kind, 0, nil
	}

	for _, pattern := range legacyCountedContextSources {
		match := pattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}

		count, err := strconv.Atoi(match[1])
		if err != nil {
			return "", 0, fmt.Errorf("the context source %q could not be counted: %w", name, err)
		}

		return legacySkillDefinitionsKind, count, nil
	}

	return "", 0, fmt.Errorf("nothing knows the context source %q", name)
}
