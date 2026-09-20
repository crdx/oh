package link

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"crdx.org/io/internal/app/escape"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
)

const (
	openPrefix   = "\x1b]8;;"
	terminator   = "\x1b\\"
	closeLink    = escape.HyperlinkClose
	ScratchAlias = "<s>"
)

const (
	pathSegment    = `[[:alnum:]_.@+%=-]+`
	pathExpression = ScratchAlias + `(?:/` + pathSegment + `)*|(?:~|\.{1,2})?/(?:` + pathSegment + `/)*` + pathSegment + `|` + pathSegment + `(?:/` + pathSegment + `)+|[[:alnum:]_@+%=-]+(?:\.[[:alnum:]_@+%=-]+)+|\.[[:alnum:]_@+%=-]+`
)

var (
	pathPattern           = regexp.MustCompile(`(` + pathExpression + `)(?::([0-9]+)(?::([0-9]+))?)?`)
	sourceLocationPattern = regexp.MustCompile(`^(.*?):([0-9]+)(?::([0-9]+))?$`)
)

func RenderURL(text string, address string) string {
	return openPrefix + address + terminator + text + closeLink
}

func RenderWebURL(text string, address string) string {
	target, isSupported := webURL(address)
	if !isSupported {
		return text
	}

	return RenderURL(text, target)
}

func webURL(address string) (string, bool) {
	target, err := url.Parse(address)
	if err != nil {
		return "", false
	}

	switch strings.ToLower(target.Scheme) {
	case "http", "https":
		if target.Host == "" {
			return "", false
		}
	case "mailto":
		if target.Opaque == "" && target.Path == "" {
			return "", false
		}
	default:
		return "", false
	}

	return target.String(), true
}

func Plain(text string) string {
	return visibleTextOf(text).text
}

type Roots struct {
	Workspace string
	Scratch   string
}

func (self Roots) IsEmpty() bool {
	return self.Workspace == "" && self.Scratch == ""
}

func (self Roots) WithoutScratch() Roots {
	self.Scratch = ""

	return self
}

func (self Roots) underScratch(path string) (string, bool) {
	if self.Scratch == "" {
		return "", false
	}

	beneath, isBeneath := pathutil.RelativeTo(sandbox.TmpDir, path)
	if !isBeneath {
		return "", false
	}

	return filepath.Join(self.Scratch, beneath), true
}

func (self Roots) underScratchAlias(path string) (string, bool) {
	if self.Scratch == "" {
		return "", false
	}

	rest, hasPrefix := strings.CutPrefix(path, ScratchAlias)
	if !hasPrefix || rest != "" && !strings.HasPrefix(rest, "/") {
		return "", false
	}

	return filepath.Join(self.Scratch, strings.TrimPrefix(rest, "/")), true
}

func Render(text string, roots Roots) string {
	visible := visibleTextOf(text)
	matches := pathPattern.FindAllStringSubmatchIndex(visible.text, -1)
	if len(matches) == 0 {
		return text
	}

	var output strings.Builder
	sourceAt := 0

	for _, match := range matches {
		found, exists := locateAround(visible.text, match[0], match[1], roots)
		if !exists || visible.hasLink(found.begin, found.end) {
			continue
		}

		begin := visible.starts[found.begin]
		end := visible.ends[found.end]
		if begin < sourceAt {
			continue
		}

		output.WriteString(text[sourceAt:begin])
		output.WriteString(openPrefix)
		output.WriteString(linkURL(found.target, found.line, found.column))
		output.WriteString(terminator)
		output.WriteString(text[begin:end])
		output.WriteString(closeLink)
		sourceAt = end
	}

	if sourceAt == 0 {
		return text
	}

	output.WriteString(text[sourceAt:])
	return output.String()
}

func PathURL(path string) string {
	return linkURL(path, "", "")
}

func RenderPath(text string, path string) string {
	return RenderURL(text, PathURL(path))
}

func RenderPathAtLine(text string, path string, roots Roots, line string) string {
	target, exists := resolve(path, roots)
	if !exists {
		return text
	}

	return RenderURL(text, linkURL(target, line, ""))
}

type visibleText struct {
	text     string
	starts   []int
	ends     []int
	isLinked []bool
}

func (self visibleText) hasLink(begin int, end int) bool {
	for _, isLinked := range self.isLinked[begin:end] {
		if isLinked {
			return true
		}
	}

	return false
}

func visibleTextOf(text string) visibleText {
	var plain strings.Builder
	starts := []int{0}
	ends := []int{0}
	var isLinked []bool
	isLinkActive := false

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			end := escapeEnd(text, i)
			sequence := text[i:end]
			if strings.HasPrefix(sequence, openPrefix) {
				isLinkActive = sequence != closeLink
			}
			i = end
			starts[len(starts)-1] = i
			continue
		}

		plain.WriteByte(text[i])
		isLinked = append(isLinked, isLinkActive)
		i++
		starts = append(starts, i)
		ends = append(ends, i)
	}

	return visibleText{text: plain.String(), starts: starts, ends: ends, isLinked: isLinked}
}

func escapeEnd(text string, start int) int {
	if start+1 >= len(text) {
		return len(text)
	}

	switch text[start+1] {
	case '[':
		for i := start + 2; i < len(text); i++ {
			if text[i] >= 0x40 && text[i] <= 0x7e {
				return i + 1
			}
		}
	case ']':
		for i := start + 2; i < len(text); i++ {
			switch {
			case text[i] == '\a':
				return i + 1
			case text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '\\':
				return i + 2
			}
		}
	default:
		return min(start+2, len(text))
	}

	return len(text)
}

type location struct {
	begin  int
	end    int
	target string
	line   string
	column string
}

func locateAround(text string, begin int, end int, roots Roots) (location, bool) {
	starts, ends := candidateBounds(text, begin, end)
	best := location{}
	wasFound := false

	for _, start := range starts {
		for _, finish := range ends {
			found, exists := locate(text[start:finish], roots)
			if !exists {
				continue
			}

			found.begin += start
			found.end += start
			if !wasFound || found.end-found.begin > best.end-best.begin {
				best = found
				wasFound = true
			}
		}
	}

	return best, wasFound
}

func candidateBounds(text string, begin int, end int) ([]int, []int) {
	segmentBegin := begin
	for segmentBegin > 0 && !isPathBoundary(text[segmentBegin-1]) {
		segmentBegin--
	}
	segmentEnd := end
	for segmentEnd < len(text) && !isPathBoundary(text[segmentEnd]) {
		segmentEnd++
	}

	starts := []int{begin}
	for at := segmentBegin; at < begin; at++ {
		isWordStart := text[at] != ' ' && (at == segmentBegin || text[at-1] == ' ')
		if isWordStart && strings.Contains(text[at:begin], " ") {
			starts = append(starts, at)
		}
	}

	ends := []int{end}
	hasCrossedSpace := false
	for at := end; at <= segmentEnd; at++ {
		if at < segmentEnd && text[at] == ' ' {
			hasCrossedSpace = true
		}
		if !hasCrossedSpace || at < segmentEnd && text[at] != ' ' {
			continue
		}
		finish := at
		for finish > end && text[finish-1] == ' ' {
			finish--
		}
		if finish != end {
			ends = append(ends, finish)
		}
	}

	return starts, ends
}

func isPathBoundary(character byte) bool {
	switch character {
	case '\n', '\r', '\t', '"', '\'', '`', '(', ')', '[', ']', '{', '}', '<', '>', ',', ';', '!', '?', '|':
		return true
	default:
		return false
	}
}

func locate(candidate string, roots Roots) (location, bool) {
	for _, end := range endings(candidate) {
		path := candidate[:end]
		if found, exists := locatePath(path, roots); exists {
			found.end = end
			return found, true
		}

		locationIndexes := sourceLocationPattern.FindStringSubmatchIndex(path)
		if locationIndexes == nil {
			continue
		}
		if found, exists := locatePath(path[locationIndexes[2]:locationIndexes[3]], roots); exists {
			found.end = end
			found.line = path[locationIndexes[4]:locationIndexes[5]]
			if locationIndexes[6] >= 0 {
				found.column = path[locationIndexes[6]:locationIndexes[7]]
			}
			return found, true
		}
	}

	return location{}, false
}

func locatePath(path string, roots Roots) (location, bool) {
	if target, exists := resolve(path, roots); exists {
		return location{target: target}, true
	}

	assignedAt := strings.LastIndexByte(path, '=') + 1
	if assignedAt > 0 && assignedAt < len(path) {
		if target, exists := resolve(path[assignedAt:], roots); exists {
			return location{begin: assignedAt, target: target}, true
		}
	}

	return location{}, false
}

func endings(candidate string) []int {
	ends := []int{len(candidate)}

	end := len(candidate)
	for end > 0 && candidate[end-1] == '.' {
		end--
	}

	if end < len(candidate) && namesSomething(candidate[:end]) {
		ends = append(ends, end)
	}

	return ends
}

func namesSomething(path string) bool {
	segment := path[strings.LastIndexByte(path, '/')+1:]

	return strings.Trim(segment, ".") != ""
}

func resolve(path string, roots Roots) (string, bool) {
	resolvedPath, isScratchAlias := roots.underScratchAlias(path)
	if !isScratchAlias {
		var err error
		resolvedPath, err = pathutil.Expand(path)
		if err != nil {
			return "", false
		}
	}

	if scratchPath, isScratch := roots.underScratch(resolvedPath); isScratch && !isScratchAlias {
		resolvedPath = scratchPath
	} else if !filepath.IsAbs(resolvedPath) {
		if roots.Workspace == "" {
			return "", false
		}

		resolvedPath = filepath.Join(roots.Workspace, resolvedPath)
	}

	absolutePath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", false
	}
	resolvedPath = absolutePath

	if _, err := os.Stat(resolvedPath); err != nil {
		return "", false
	}

	return resolvedPath, true
}

func linkURL(path string, line string, column string) string {
	address := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if line != "" {
		address.Fragment = line
		if column != "" {
			address.Fragment += ":" + column
		}
	}

	return address.String()
}
