package edit

import (
	"encoding/json"
	"regexp"
	"strings"

	"crdx.org/io/internal/util/strutil"
)

const minimumCodeBlockPasteLines = 6

var (
	goPackagePattern      = regexp.MustCompile(`(?m)^package [[:alpha:]_][[:alnum:]_]*[ \t]*$`)
	goDeclarationPattern  = regexp.MustCompile(`(?m)^(?:import[ \t]+(?:["(]|[[:alpha:]_])|func[ \t]+[[:alpha:]_][[:alnum:]_]*[ \t]*\(|type[ \t]+[[:alpha:]_][[:alnum:]_]*[ \t]+(?:struct|interface)\b)`)
	pythonFunctionPattern = regexp.MustCompile(`(?m)^(?:async[ \t]+)?def[ \t]+[[:alpha:]_][[:alnum:]_]*[ \t]*\([^()\n]*\)[ \t]*(?:->[ \t]*[^:\n]+)?[ \t]*:[ \t]*$`)
	dockerBasePattern     = regexp.MustCompile(`(?i)^FROM[ \t]+[^ \t\n]+`)
	dockerStepPattern     = regexp.MustCompile(`(?mi)^(?:RUN|COPY|ADD|ENTRYPOINT|CMD)[ \t]+`)
)

func preparePastedText(text string, isAtLineStart bool, isAtLineEnd bool) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = normaliseIndentation(strutil.StripControl(text))

	if pastedLineCount(text) < minimumCodeBlockPasteLines {
		return text
	}

	codeBlock := FencedCodeBlock(text)

	if !isAtLineStart {
		codeBlock = "\n" + codeBlock
	}
	if !isAtLineEnd {
		codeBlock += "\n"
	}

	return codeBlock
}

func FencedCodeBlock(text string) string {
	fence := strings.Repeat("`", max(3, longestBacktickRun(text)+1))
	openingFence := fence + pasteLanguage(text)
	codeBlock := openingFence + "\n" + text
	if !strings.HasSuffix(codeBlock, "\n") {
		codeBlock += "\n"
	}

	return codeBlock + fence
}

func pastedLineCount(text string) int {
	text = strings.TrimSuffix(text, "\n")
	return strings.Count(text, "\n") + 1
}

func longestBacktickRun(text string) int {
	longest := 0
	current := 0
	for _, character := range text {
		if character == '`' {
			current++
			longest = max(longest, current)
			continue
		}
		current = 0
	}
	return longest
}

func pasteLanguage(text string) string {
	trimmedText := strings.TrimSpace(text)
	if language := pasteShebangLanguage(trimmedText); language != "" {
		return language
	}

	switch {
	case strings.HasPrefix(trimmedText, "diff --git "):
		return "diff"
	case strings.HasPrefix(strings.ToLower(trimmedText), "<!doctype html>"):
		return "html"
	case strings.HasPrefix(trimmedText, "<?xml "):
		return "xml"
	case strings.HasPrefix(trimmedText, "<?php"):
		return "php"
	case isJSONObjectOrArray(trimmedText):
		return "json"
	case goPackagePattern.MatchString(text) && goDeclarationPattern.MatchString(text):
		return "go"
	case pythonFunctionPattern.MatchString(text):
		return "python"
	case dockerBasePattern.MatchString(trimmedText) && dockerStepPattern.MatchString(text):
		return "dockerfile"
	default:
		return ""
	}
}

func pasteShebangLanguage(text string) string {
	firstLine, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(firstLine, "#!") {
		return ""
	}

	words := strings.Fields(strings.TrimPrefix(firstLine, "#!"))
	if len(words) == 0 {
		return ""
	}

	executable := words[0][strings.LastIndex(words[0], "/")+1:]
	if executable == "env" {
		executable = ""
		for _, word := range words[1:] {
			switch {
			case word == "-S" || strings.Contains(word, "="):
				continue
			case strings.HasPrefix(word, "-"):
				return ""
			default:
				executable = word[strings.LastIndex(word, "/")+1:]
			}
			break
		}
	}

	switch {
	case executable == "bash":
		return "bash"
	case executable == "sh":
		return "sh"
	case executable == "python" || executable == "python3" || strings.HasPrefix(executable, "python3."):
		return "python"
	case executable == "ruby":
		return "ruby"
	case executable == "perl":
		return "perl"
	case executable == "php":
		return "php"
	case executable == "lua":
		return "lua"
	case executable == "goscript":
		return "go"
	default:
		return ""
	}
}

func isJSONObjectOrArray(text string) bool {
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return false
	}
	return json.Valid([]byte(text))
}
