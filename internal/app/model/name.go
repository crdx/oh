package model

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util"
)

var spellings = map[string]string{
	"ai":       "AI",
	"deepseek": "DeepSeek",
	"glm":      "GLM",
	"gpt":      "GPT",
	"hy":       "HY",
	"llm":      "LLM",
	"moe":      "MoE",
	"o":        "o",
	"ocr":      "OCR",
	"oss":      "OSS",
	"longcat":  "LongCat",
	"vl":       "VL",
	"xl":       "XL",
	"xs":       "XS",
}

var providerNames = map[string]string{
	AnthropicProvider:  "Anthropic",
	CodexProvider:      "Codex",
	OllamaProvider:     "Ollama",
	OpencodeGoProvider: "OpenCode Go",
}

var standalone = map[string]bool{
	"codex":  true,
	"fable":  true,
	"opus":   true,
	"sonnet": true,
}

const joinedLetters = 2

var (
	iterationWord = regexp.MustCompile(`^([a-z]?)([0-9]+(\.[0-9]+)*)$`)
	trailingCount = regexp.MustCompile(`^([a-z.]+?)([0-9]+(\.[0-9]+)*)$`)
	parameterSize = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[bm]$`)
	contextLength = regexp.MustCompile(`^[0-9]+k$`)
	quantityWord  = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[a-z]$`)
)

func ProviderName(id string) string {
	if name, isFound := providerNames[id]; isFound {
		return name
	}

	return capitalise(id)
}

func DisplayName(id string) []string {
	base, tag := splitIdentifier(id)

	name := readable(derivedName(base, tag))
	if len(name) == 0 {
		return []string{plainly(id)}
	}

	return name
}

func ShortName(id string) []string {
	name := slices.Clone(DisplayName(id))
	name[0] = initials(name[0])

	return name
}

func initials(name string) string {
	var letters strings.Builder

	for word := range strings.FieldsSeq(name) {
		clusters := graphemes(word)

		at := firstSeen(clusters)
		if at == len(clusters) {
			continue
		}

		letters.WriteString(clusters[at])

		for _, cluster := range clusters[at+1:] {
			if isCapital(cluster) {
				letters.WriteString(cluster)
			}
		}

		letters.WriteString(trailingNumber(clusters[at+1:]))
	}

	if letters.Len() == 0 {
		return name
	}

	return letters.String()
}

func graphemes(word string) []string {
	var clusters []string

	for cluster := range width.Graphemes(word) {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func firstSeen(clusters []string) int {
	for at, cluster := range clusters {
		if width.Of(cluster) > 0 {
			return at
		}
	}

	return len(clusters)
}

func trailingNumber(clusters []string) string {
	at := len(clusters)

	for at > 0 && isNumeric(clusters[at-1]) {
		at--
	}

	return strings.Join(clusters[at:], "")
}

func isCapital(cluster string) bool {
	character, _ := utf8.DecodeRuneInString(cluster)

	return unicode.IsUpper(character)
}

func isNumeric(cluster string) bool {
	character, size := utf8.DecodeRuneInString(cluster)
	if size != len(cluster) {
		return false
	}

	return unicode.IsDigit(character) || character == '.'
}

func readable(name []string) []string {
	keptParts := make([]string, 0, len(name))

	for _, part := range name {
		if part = plainly(part); part != "" {
			keptParts = append(keptParts, part)
		}
	}

	return keptParts
}

func plainly(text string) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}

		return character
	}, text))
}

func splitIdentifier(id string) (string, string) {
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		id = id[slash+1:]
	}

	base, tag, _ := strings.Cut(id, ":")

	return strings.TrimSpace(base), strings.TrimSpace(tag)
}

func derivedName(base string, tag string) []string {
	var words []string
	var iteration string
	var isFollowingIteration bool

	for at, word := range strings.Split(base, "-") {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}

		if util.IsDatedSnapshot(word) {
			isFollowingIteration = false
			continue
		}

		if countedWord, isIteration := readIteration(word, at == 0); isIteration {
			switch {
			case iteration == "":
				iteration = countedWord
			case isFollowingIteration:
				iteration += "." + countedWord
			}

			isFollowingIteration = true

			continue
		}

		if writtenWord, countedNumber, hasCount := readCountedWord(word); hasCount {
			words = append(words, writtenWord)
			if iteration == "" {
				iteration = countedNumber
			}

			isFollowingIteration = countedNumber != ""

			continue
		}

		words = append(words, capitalise(word))
		isFollowingIteration = false
	}

	words = fromStandalone(words)

	for _, measure := range []string{sizeOf(tag), contextOf(tag)} {
		if measure != "" {
			iteration = strings.TrimSpace(iteration + " " + measure)
		}
	}

	if len(words) == 0 {
		return []string{base}
	}

	name := strings.Join(words, " ")
	if iteration == "" {
		return []string{name}
	}

	return []string{name, iteration}
}

func readIteration(word string, isFirst bool) (string, bool) {
	parts := iterationWord.FindStringSubmatch(word)
	if parts == nil {
		return "", false
	}

	letter := parts[1]

	switch {
	case letter == "" || letter == "v":
		return parts[2], true
	case isFirst:
		return "", false
	}

	return strings.ToUpper(letter) + parts[2], true
}

func readCountedWord(word string) (string, string, bool) {
	parts := trailingCount.FindStringSubmatch(word)
	if parts == nil {
		return "", "", false
	}

	letters, countedNumber := parts[1], parts[2]
	if len(letters) <= joinedLetters {
		return capitalise(letters) + countedNumber, "", true
	}

	return capitalise(letters), countedNumber, true
}

func fromStandalone(words []string) []string {
	for at, word := range words {
		if standalone[strings.ToLower(word)] {
			return words[at:]
		}
	}

	return words
}

func sizeOf(tag string) string {
	first, _, _ := strings.Cut(tag, "-")
	if !parameterSize.MatchString(first) {
		return ""
	}

	return strings.ToUpper(first)
}

func contextOf(tag string) string {
	for token := range strings.SplitSeq(tag, "-") {
		if contextLength.MatchString(token) {
			return strings.ToUpper(token)
		}
	}

	return ""
}

func capitalise(word string) string {
	if knownSpelling, isFound := spellings[word]; isFound {
		return knownSpelling
	}
	if quantityWord.MatchString(word) {
		return strings.ToUpper(word)
	}

	first, size := utf8.DecodeRuneInString(word)
	if size == 0 {
		return word
	}

	return string(unicode.ToUpper(first)) + word[size:]
}
