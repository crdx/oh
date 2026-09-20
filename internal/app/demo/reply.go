package demo

import (
	"encoding/json"
	"fmt"
	"strings"

	"crdx.org/io/internal/sim"
)

func Tools() []string {
	return []string{readTool, listTool, grepTool}
}

const (
	readTool = "read"
	listTool = "ls"
	grepTool = "grep"
)

const (
	openingThought  = "Somebody is trying oh out, so I should say what I am."
	readingThought  = "They named a path, so the read tool is the one to reach for."
	listingThought  = "They want to know what is here, and ls answers that."
	searchThought   = "They are looking for something, so grep it is."
	refusalThought  = "That needs a model that can think, and I cannot."
	reportThought   = "The tool has answered, so I should say what came back."
	markdownThought = "Markdown is where the drawing earns its keep."
	doctorThought   = "Nothing I know matched, so the doctor can take it from here."
)

const introduction = "I'm a simulation, and not a model at all. " +
	"I match a few words in what you write and reach for a tool when I recognise one, " +
	"so you can watch how oh draws a conversation. Nothing here is saved."

const abilities = "I know how to do three things, and only by the words you use:\n\n" +
	"- **read** a path you name, as in `read main.go`\n" +
	"- **list** what is here, as in `what files are here`\n" +
	"- **search** for a word, as in `grep for wizard`\n\n" +
	"Anything else and you'll be answered by the DOCTOR script Weizenbaum published in 1966, " +
	"which listens rather better than I do."

const markdownShowcase = `# What you're looking at

The **answer** is streamed as it arrives, and ` + "`code`" + ` is drawn as it lands:

` + "```go" + `
func main() {
	fmt.Println("hello")
}
` + "```" + `

| Part      | Where it comes from |
|:----------|:--------------------|
| answer    | a table of phrases  |
| tools     | the harness         |
| the words | you                 |

` + "```mermaid" + `
graph LR
you[you] --> oh[oh] --> simulation[simulation]
` + "```" + `

> A model would have read your message first. I only matched a word in it.`

type request struct {
	messages   []string
	message    string
	toolName   string
	toolOutput string
	hasOutput  bool
}

func answer(conversation sim.Request) sim.Turn {
	return replyTo(readRequest(conversation))
}

func readRequest(conversation sim.Request) request {
	self := request{}

	calls := map[string]string{}

	for _, entry := range conversation.Input {
		switch entry.Type {
		case sim.Message:
			if entry.Role == "user" && strings.TrimSpace(entry.Content) != "" {
				self.messages = append(self.messages, entry.Content)
				self.message = entry.Content
				self.hasOutput = false
			}

		case sim.CallMade:
			calls[entry.CallID] = entry.Name

		case sim.CallOutput:
			self.toolName = calls[entry.CallID]
			self.toolOutput = entry.Output
			self.hasOutput = true
		}
	}

	return self
}

func replyTo(enquiry request) sim.Turn {
	if enquiry.hasOutput {
		return report(enquiry)
	}

	if turn, isMatch := matchedReply(enquiry.message); isMatch {
		return turn
	}

	return doctorReply(enquiry.messages)
}

func matchedReply(sentence string) (sim.Turn, bool) {
	message := strings.ToLower(sentence)

	switch {
	case strings.TrimSpace(message) == "":
		return sim.Turn{Say: "You said nothing at all, and I have nothing to match."}, true

	case mentions(message, "hello", "hi", "hey", "morning", "afternoon", "evening"):
		return sim.Turn{
			Think: []string{openingThought},
			Say:   "Hello. " + introduction + "\n\n" + abilities,
		}, true

	case mentions(message, "who are you", "what are you", "are you real", "are you an llm", "are you a model"):
		return sim.Turn{Think: []string{openingThought}, Say: introduction}, true

	case mentions(message, "what can you do", "help me", "how does this work", "what do you do"):
		return sim.Turn{Say: abilities}, true

	case mentions(message, "markdown", "table", "diagram", "mermaid", "show off"):
		return sim.Turn{Think: []string{markdownThought}, Say: markdownShowcase}, true

	case mentions(message, "read", "open", "show me", "look at", "cat"):
		return readReply(sentence), true

	case mentions(message, "ls", "list", "files", "directory", "what is here", "what's here"):
		return listReply(sentence), true

	case mentions(message, "grep", "search", "find", "look for", "where is"):
		return searchReply(sentence), true

	case mentions(message, "write", "edit", "change", "fix", "refactor", "run", "bash"):
		return sim.Turn{
			Think: []string{refusalThought},
			Say: "I can only read, list, and search. " +
				"Changing anything, or running anything, wants a model that understands what you asked.",
		}, true
	}

	return sim.Turn{}, false
}

func doctorReply(messages []string) sim.Turn {
	eliza, err := consultDoctor()
	if err != nil {
		return sim.Turn{
			Think: []string{doctorThought},
			Say: "None of my few words matched that. " +
				"Try `read <path>`, `what files are here`, or `search for <word>`.",
		}
	}

	reply := ""

	for _, message := range messages {
		if _, isMatch := matchedReply(message); !isMatch {
			reply = speak(eliza.Reply(message))
		}
	}

	return sim.Turn{Think: []string{doctorThought}, Say: reply}
}

func readReply(message string) sim.Turn {
	path, hasPath := pathIn(message)
	if !hasPath {
		return sim.Turn{
			Think: []string{refusalThought},
			Say:   "Name a path and I'll read it, as in `read main.go`.",
		}
	}

	return sim.Turn{
		Think: []string{readingThought},
		Say:   "Reading " + quote(path) + " now.",
		Calls: []sim.Call{{Name: readTool, Arguments: encode(map[string]string{"path": path})}},
	}
}

func listReply(message string) sim.Turn {
	values := map[string]string{}
	if path, hasPath := pathIn(message); hasPath {
		values["path"] = path
	}

	return sim.Turn{
		Think: []string{listingThought},
		Say:   "Here's what's there.",
		Calls: []sim.Call{{Name: listTool, Arguments: encode(values)}},
	}
}

func searchReply(message string) sim.Turn {
	pattern, hasPattern := patternIn(message)
	if !hasPattern {
		return sim.Turn{
			Think: []string{refusalThought},
			Say:   "Give me a word to look for, as in `search for wizard`.",
		}
	}

	return sim.Turn{
		Think: []string{searchThought},
		Say:   "Looking for " + quote(pattern) + " across the workspace.",
		Calls: []sim.Call{{Name: grepTool, Arguments: encode(map[string]string{"pattern": pattern})}},
	}
}

func report(enquiry request) sim.Turn {
	lines := countLines(enquiry.toolOutput)

	switch enquiry.toolName {
	case readTool:
		return sim.Turn{
			Think: []string{reportThought},
			Say: fmt.Sprintf(
				"That's %s. A model would tell you what it means; I can only tell you it arrived.",
				measured(lines, "line", "lines"),
			),
		}

	case listTool:
		return sim.Turn{
			Think: []string{reportThought},
			Say: fmt.Sprintf(
				"There are %s there. Name one and I'll read it.",
				measured(lines, "entry", "entries"),
			),
		}

	case grepTool:
		if lines == 0 {
			return sim.Turn{Think: []string{reportThought}, Say: "Nothing matched, so there's nothing to say about it."}
		}

		return sim.Turn{
			Think: []string{reportThought},
			Say:   fmt.Sprintf("That's %s. The path and line number are on each one.", measured(lines, "match", "matches")),
		}
	}

	return sim.Turn{Think: []string{reportThought}, Say: "The tool answered, and I have nothing to add to it."}
}

func mentions(message string, phrases ...string) bool {
	for _, phrase := range phrases {
		if wholeWords(message, phrase) {
			return true
		}
	}

	return false
}

func wholeWords(message string, phrase string) bool {
	for at := 0; at <= len(message)-len(phrase); {
		found := strings.Index(message[at:], phrase)
		if found < 0 {
			return false
		}

		start := at + found
		end := start + len(phrase)

		if !isWordAt(message, start-1) && !isWordAt(message, end) {
			return true
		}

		at = start + 1
	}

	return false
}

func isWordAt(message string, at int) bool {
	if at < 0 || at >= len(message) {
		return false
	}

	return isWordRune(rune(message[at]))
}

func isWordRune(character rune) bool {
	return character == '_' ||
		(character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9')
}

func pathIn(message string) (string, bool) {
	for word := range strings.FieldsSeq(message) {
		candidate := strings.Trim(word, "`'\"“”‘’.,:;!?()[]")
		if candidate == "" {
			continue
		}
		if strings.ContainsAny(candidate, "/") || strings.Contains(candidate, ".") {
			return candidate, true
		}
	}

	return "", false
}

func patternIn(message string) (string, bool) {
	if phrase, hasPhrase := betweenMarks(message); hasPhrase {
		return phrase, true
	}

	words := strings.Fields(message)
	for i, word := range words {
		if strings.EqualFold(word, "for") || strings.EqualFold(word, "is") {
			if i+1 < len(words) {
				return strings.Trim(words[i+1], "`'\"“”‘’.,:;!?()[]"), true
			}
		}
	}

	if len(words) > 1 {
		return strings.Trim(words[len(words)-1], "`'\"“”‘’.,:;!?()[]"), true
	}

	return "", false
}

func betweenMarks(message string) (string, bool) {
	for _, mark := range []string{"`", `"`, "'"} {
		start := strings.Index(message, mark)
		if start < 0 {
			continue
		}

		width := strings.Index(message[start+1:], mark)
		if width <= 0 {
			continue
		}

		return message[start+1 : start+1+width], true
	}

	return "", false
}

func countLines(output string) int {
	text := strings.TrimRight(output, "\n")
	if text == "" {
		return 0
	}

	return strings.Count(text, "\n") + 1
}

func measured(count int, subject string, subjects string) string {
	if count == 1 {
		return "one " + subject
	}

	return fmt.Sprintf("%d %s", count, subjects)
}

func quote(text string) string {
	return "`" + strings.TrimSpace(text) + "`"
}

func encode(values map[string]string) string {
	document, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}

	return string(document)
}
