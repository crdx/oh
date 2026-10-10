package link

import (
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"crdx.org/oh/internal/app/escape"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
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
	pathPattern    = regexp.MustCompile(`(` + pathExpression + `)(?::([0-9]+)(?::([0-9]+))?)?`)
	addressPattern = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.-]*://[^\\s'\"`<>|;]+")
)

const addressClosingPunctuation = ".,;:!?"

type Bounds struct {
	Begin int
	End   int
}

func Addresses(text string) []Bounds {
	if !strings.Contains(text, "://") {
		return nil
	}

	var found []Bounds
	for _, place := range addressPattern.FindAllStringIndex(text, -1) {
		found = append(found, Bounds{Begin: place[0], End: addressEnd(text[place[0]:place[1]]) + place[0]})
	}

	return found
}

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

	return renderLocations(text, visible, locations(visible.text, roots))
}

type Source struct {
	text      string
	locations []location
}

func Locate(text string, roots Roots) Source {
	sourceText := visibleTextOf(text).text

	return Source{text: sourceText, locations: locations(sourceText, roots)}
}

func (self Source) IsZero() bool {
	return self.text == ""
}

func RenderFromSource(text string, source Source, roots Roots) string {
	visible := visibleTextOf(text)
	prefixEnd := commonPrefixEnd(visible.text, source.text)
	if prefixEnd == len(source.text) {
		if len(visible.text) == len(source.text) {
			return renderLocations(text, visible, source.locations)
		}

		return Render(text, roots)
	}

	shownLocations := make([]location, 0, len(source.locations))
	for _, found := range source.locations {
		if found.begin >= prefixEnd {
			continue
		}
		if found.end > prefixEnd {
			found.end = len(visible.text)
		}
		shownLocations = append(shownLocations, found)
	}

	return renderLocations(text, visible, shownLocations)
}

func commonPrefixEnd(first string, second string) int {
	end := 0
	for end < len(first) && end < len(second) && first[end] == second[end] {
		end++
	}

	return end
}

func locations(text string, roots Roots) []location {
	addresses := webAddresses(text)
	matches := pathMatches(text)
	foundLocations := make([]location, 0, len(matches)+len(addresses))
	var candidateLocations map[string]candidateLocation
	if len(matches) > 1 {
		candidateLocations = make(map[string]candidateLocation, len(matches))
	}
	lastEnd := 0

	for _, match := range matches {
		if overlapsAny(addresses, match[0], match[1]) {
			continue
		}

		found, exists := locateAround(text, match[0], match[1], roots, candidateLocations)
		if !exists || found.begin < lastEnd || overlapsAny(addresses, found.begin, found.end) {
			continue
		}

		foundLocations = append(foundLocations, found)
		lastEnd = found.end
	}

	if len(addresses) == 0 {
		return foundLocations
	}

	foundLocations = append(foundLocations, addresses...)
	sort.Slice(foundLocations, func(first int, second int) bool {
		return foundLocations[first].begin < foundLocations[second].begin
	})

	return foundLocations
}

func addressEnd(address string) int {
	end := len(address)
	for end > 0 {
		switch last := address[end-1]; {
		case strings.IndexByte(addressClosingPunctuation, last) >= 0:
			end--
		case last == ')' && strings.Count(address[:end], ")") > strings.Count(address[:end], "("):
			end--
		default:
			return end
		}
	}

	return end
}

func webAddresses(text string) []location {
	var found []location
	for _, bounds := range Addresses(text) {
		target, isSupported := webURL(text[bounds.Begin:bounds.End])
		if !isSupported {
			continue
		}

		found = append(found, location{begin: bounds.Begin, end: bounds.End, address: target})
	}

	return found
}

func overlapsAny(addresses []location, begin int, end int) bool {
	for _, address := range addresses {
		if address.begin < end && begin < address.end {
			return true
		}
	}

	return false
}

func renderLocations(text string, visible visibleText, foundLocations []location) string {
	var output strings.Builder
	sourceAt := 0

	for _, found := range foundLocations {
		if visible.hasLink(found.begin, found.end) {
			continue
		}

		begin := visible.sourceStart(found.begin)
		end := visible.sourceEnd(found.end)
		if begin < sourceAt {
			continue
		}

		output.WriteString(text[sourceAt:begin])
		output.WriteString(openPrefix)
		output.WriteString(found.url())
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
	text         string
	spans        []visibleSpan
	sourceLength int
	isPlain      bool
}

type visibleSpan struct {
	visibleBegin int
	visibleEnd   int
	sourceBegin  int
	isLinked     bool
}

func (self visibleText) hasLink(begin int, end int) bool {
	if self.isPlain {
		return false
	}

	at := sort.Search(len(self.spans), func(index int) bool {
		return self.spans[index].visibleEnd > begin
	})
	for at < len(self.spans) && self.spans[at].visibleBegin < end {
		if self.spans[at].isLinked {
			return true
		}
		at++
	}

	return false
}

func (self visibleText) sourceStart(at int) int {
	if self.isPlain {
		return at
	}

	index := sort.Search(len(self.spans), func(index int) bool {
		return self.spans[index].visibleEnd > at
	})
	if index == len(self.spans) {
		return self.sourceLength
	}

	span := self.spans[index]
	return span.sourceBegin + at - span.visibleBegin
}

func (self visibleText) sourceEnd(at int) int {
	if self.isPlain {
		return at
	}

	index := sort.Search(len(self.spans), func(index int) bool {
		return self.spans[index].visibleEnd >= at
	})
	if index == len(self.spans) {
		return self.sourceLength
	}

	span := self.spans[index]
	return span.sourceBegin + at - span.visibleBegin
}

func visibleTextOf(text string) visibleText {
	if !strings.ContainsRune(text, '\x1b') {
		return visibleText{text: text, sourceLength: len(text), isPlain: true}
	}

	var plain strings.Builder
	var spans []visibleSpan
	isLinkActive := false

	for sourceAt := 0; sourceAt < len(text); {
		if text[sourceAt] == '\x1b' {
			end := escapeEnd(text, sourceAt)
			sequence := text[sourceAt:end]
			if strings.HasPrefix(sequence, openPrefix) {
				isLinkActive = sequence != closeLink
			}
			sourceAt = end
			continue
		}

		end := strings.IndexByte(text[sourceAt:], '\x1b')
		if end < 0 {
			end = len(text)
		} else {
			end += sourceAt
		}

		visibleBegin := plain.Len()
		plain.WriteString(text[sourceAt:end])
		spans = append(spans, visibleSpan{
			visibleBegin: visibleBegin,
			visibleEnd:   plain.Len(),
			sourceBegin:  sourceAt,
			isLinked:     isLinkActive,
		})
		sourceAt = end
	}

	return visibleText{text: plain.String(), spans: spans, sourceLength: len(text)}
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
	begin   int
	end     int
	target  string
	line    string
	column  string
	address string
}

func (self location) url() string {
	if self.address != "" {
		return self.address
	}

	return linkURL(self.target, self.line, self.column)
}

type candidateLocation struct {
	value  location
	exists bool
}

func locateAround(
	text string,
	begin int,
	end int,
	roots Roots,
	candidateLocations map[string]candidateLocation,
) (location, bool) {
	starts, ends := candidateBounds(text, begin, end)
	best := location{}
	wasFound := false

	for _, start := range starts {
		for _, finish := range ends {
			found, exists := locateCandidate(text[start:finish], roots, candidateLocations)
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

func locateCandidate(
	candidate string,
	roots Roots,
	candidateLocations map[string]candidateLocation,
) (location, bool) {
	if candidateLocations == nil {
		return locate(candidate, roots)
	}

	if found, isCached := candidateLocations[candidate]; isCached {
		return found.value, found.exists
	}

	value, exists := locate(candidate, roots)
	candidateLocations[candidate] = candidateLocation{value: value, exists: exists}
	return value, exists
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
	lastSpace := strings.LastIndexByte(text[segmentBegin:begin], ' ') + segmentBegin
	for at := segmentBegin; at < lastSpace; at++ {
		isWordStart := text[at] != ' ' && (at == segmentBegin || text[at-1] == ' ')
		if isWordStart {
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

		sourcePath, line, column, isLocation := splitSourceLocation(path)
		if !isLocation {
			continue
		}
		if found, exists := locatePath(sourcePath, roots); exists {
			found.end = end
			found.line = line
			found.column = column
			return found, true
		}
	}

	return location{}, false
}

func splitSourceLocation(value string) (string, string, string, bool) {
	lastColon := strings.LastIndexByte(value, ':')
	if lastColon < 0 || !isDecimal(value[lastColon+1:]) {
		return "", "", "", false
	}

	sourcePath := value[:lastColon]
	line := value[lastColon+1:]
	column := ""

	if previousColon := strings.LastIndexByte(sourcePath, ':'); previousColon >= 0 && isDecimal(sourcePath[previousColon+1:]) {
		column = line
		line = sourcePath[previousColon+1:]
		sourcePath = sourcePath[:previousColon]
	}

	return sourcePath, line, column, true
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}

	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}

	return true
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

	if !filepath.IsAbs(resolvedPath) {
		absolutePath, err := filepath.Abs(resolvedPath)
		if err != nil {
			return "", false
		}
		resolvedPath = absolutePath
	}

	if !existenceMemory.exists(resolvedPath) {
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
