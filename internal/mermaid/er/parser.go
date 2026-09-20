package er

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"crdx.org/oh/internal/mermaid/diagram"
)

const erKeyword = "erDiagram"

type Cardinality int

const (
	OnlyOne Cardinality = iota
	ZeroOrOne
	ZeroOrMore
	OneOrMore
)

type Attribute struct {
	Type    string
	Name    string
	Keys    []string
	Comment string
}

type Entity struct {
	Name       string
	Display    string
	Attributes []Attribute
}

type Relationship struct {
	Left, Right string
	LeftCard    Cardinality
	RightCard   Cardinality
	Identifying bool
	Label       string
}

type ErDiagram struct {
	Entities      []*Entity
	Relationships []*Relationship
	byName        map[string]*Entity
}

var (
	cardAny = map[string]Cardinality{
		"||": OnlyOne,
		"|o": ZeroOrOne, "o|": ZeroOrOne,
		"}o": ZeroOrMore, "o{": ZeroOrMore,
		"}|": OneOrMore, "|{": OneOrMore,
		"1": OnlyOne, "only one": OnlyOne, "one": OnlyOne,
		"zero or one": ZeroOrOne, "one or zero": ZeroOrOne,
		"0+": ZeroOrMore, "zero or more": ZeroOrMore, "zero or many": ZeroOrMore,
		"many": ZeroOrMore, "many(0)": ZeroOrMore,
		"1+": OneOrMore, "one or more": OneOrMore, "one or many": OneOrMore, "many(1)": OneOrMore,
	}

	lineOpRegex = regexp.MustCompile(`[-.]{2}`)

	directionRegex = regexp.MustCompile(`(?i)^\s*direction\s+\S+\s*$`)

	styleLineRegex = regexp.MustCompile(`(?i)^\s*(classDef|class|style)\b`)

	accLineRegex = regexp.MustCompile(`(?i)^\s*(accTitle|accDescr)\s*[:{]`)

	entityHeaderRegex = regexp.MustCompile(`^\s*(?:"([^"]+)"|([^\s{}["]+))(?:\s*\[\s*"?([^"\]]+?)"?\s*\]|\s+(\S+))?\s*\{\s*$`)

	loneEntityRegex = regexp.MustCompile(`^\s*(?:"([^"]+)"|([^\s{}:|"\[]+))(?:\s*\[\s*"?([^"\]]+?)"?\s*\]|\s+(\S+))?\s*$`)

	attrKeyRegex = regexp.MustCompile(`^(?:PK|FK|UK)(?:\s*,\s*(?:PK|FK|UK))*$`)

	emptyBlockRegex = regexp.MustCompile(`\s*\{\s*\}\s*$`)

	classShorthandRegex = regexp.MustCompile(`:::[\w,-]+`)

	subgraphRegex = regexp.MustCompile(`^subgraph\b`)
)

func IsErDiagram(input string) bool {
	for line := range strings.SplitSeq(input, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		low := strings.ToLower(t)
		return low == strings.ToLower(erKeyword) ||
			strings.HasPrefix(low, strings.ToLower(erKeyword)+" ")
	}
	return false
}

func (self *ErDiagram) entity(name string) *Entity {
	if e, ok := self.byName[name]; ok {
		return e
	}
	e := &Entity{Name: name, Display: name}
	self.byName[name] = e
	self.Entities = append(self.Entities, e)
	return e
}

func Parse(input string) (*ErDiagram, error) {
	if !IsErDiagram(input) {
		return nil, fmt.Errorf("expected %q keyword", erKeyword)
	}
	lines := diagram.SplitLines(strings.TrimSpace(input))
	for i, l := range lines {
		lines[i] = stripComment(l)
	}

	diagram := &ErDiagram{byName: map[string]*Entity{}}

	hasSeenKeyword := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if !hasSeenKeyword {
			hasSeenKeyword = true
			continue
		}

		if accLineRegex.MatchString(line) {
			if strings.HasSuffix(line, "{") {
				for i++; i < len(lines) && !strings.Contains(lines[i], "}"); i++ {
				}
			}
			continue
		}
		if directionRegex.MatchString(line) {
			continue
		}

		if subgraphRegex.MatchString(line) || line == "end" {
			return nil, fmt.Errorf("line %d: er subgraphs are not supported", i+1)
		}

		if strings.Contains(line, ":::") {
			line = stripClassShorthand(line)
		}

		if emptyBlockRegex.MatchString(line) {
			line = strings.TrimSpace(emptyBlockRegex.ReplaceAllString(line, ""))
		}

		if m := entityHeaderRegex.FindStringSubmatch(line); m != nil {
			name := firstNonEmpty(m[1], m[2])
			entity := diagram.entity(name)
			if alias := firstNonEmpty(m[3], m[4]); alias != "" {
				entity.Display = alias
			}
			attrs, next, err := parseAttributeBlock(lines, i+1)
			if err != nil {
				return nil, fmt.Errorf("entity %q: %w", name, err)
			}
			entity.Attributes = append(entity.Attributes, attrs...)
			i = next
			continue
		}

		if diagram.parseRelationship(line) {
			continue
		}

		if styleLineRegex.MatchString(line) {
			continue
		}

		if m := loneEntityRegex.FindStringSubmatch(line); m != nil {
			e := diagram.entity(firstNonEmpty(m[1], m[2]))
			if alias := firstNonEmpty(m[3], m[4]); alias != "" {
				e.Display = alias
			}
			continue
		}

		return nil, fmt.Errorf("line %d: invalid syntax: %q", i+1, line)
	}

	return diagram, nil
}

func parseAttributeBlock(lines []string, start int) ([]Attribute, int, error) {
	var attrs []Attribute
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		closes := strings.HasSuffix(line, "}")
		if closes {
			line = strings.TrimSpace(strings.TrimSuffix(line, "}"))
		}
		if line != "" {
			attr, err := parseAttribute(line)
			if err != nil {
				return nil, i, fmt.Errorf("line %d: %w", i+1, err)
			}
			attrs = append(attrs, attr)
		}
		if closes {
			return attrs, i, nil
		}
	}
	return nil, len(lines), errors.New("unclosed attribute block (missing '}')")
}

func parseAttribute(line string) (Attribute, error) {
	comment := ""
	if index := strings.Index(line, `"`); index != -1 {
		if end := strings.LastIndex(line, `"`); end > index {
			comment = line[index+1 : end]
		} else {
			comment = line[index+1:]
		}
		line = strings.TrimSpace(line[:index])
	}
	fields := splitAttrTokens(line)
	if len(fields) < 2 {
		return Attribute{}, fmt.Errorf("attribute needs a type and name: %q", line)
	}
	attr := Attribute{
		Type:    strings.Trim(fields[0], "`"),
		Name:    strings.Trim(fields[1], "`"),
		Comment: comment,
	}
	if rest := strings.TrimSpace(strings.Join(fields[2:], " ")); rest != "" {
		if !attrKeyRegex.MatchString(rest) {
			return Attribute{}, fmt.Errorf("unexpected attribute tokens %q", rest)
		}
		for k := range strings.SplitSeq(rest, ",") {
			attr.Keys = append(attr.Keys, strings.TrimSpace(k))
		}
	}
	return attr, nil
}

func stripClassShorthand(line string) string {
	parts := strings.Split(line, `"`)
	for i := 0; i < len(parts); i += 2 {
		parts[i] = classShorthandRegex.ReplaceAllString(parts[i], "")
	}
	return strings.Join(parts, `"`)
}

func stripComment(line string) string {
	isInQuote := false
	for i := range len(line) {
		switch {
		case line[i] == '"':
			isInQuote = !isInQuote
		case !isInQuote && line[i] == '%' && i+1 < len(line) && line[i+1] == '%':
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}

func firstNonEmpty(a string, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (self *ErDiagram) parseRelationship(line string) bool {
	before, after, ok := strings.Cut(line, ":")
	if !ok {
		return false
	}
	main := strings.TrimSpace(before)
	label := strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(after), `"`)), " ")

	var left, right string
	var isIdentifying bool
	if loc := lineOpRegex.FindStringIndex(main); loc != nil {
		left = strings.TrimSpace(main[:loc[0]])
		right = strings.TrimSpace(main[loc[1]:])
		isIdentifying = main[loc[0]:loc[1]] == "--"
	} else if i, w := findWordOp(main); i >= 0 {
		left = strings.TrimSpace(main[:i])
		right = strings.TrimSpace(main[i+len(w):])
		isIdentifying = w == " to "
	} else {
		return false
	}

	e1, lcard := splitEntityCard(left, true)
	e2, rcard := splitEntityCard(right, false)
	lc, isLeftKnown := cardAny[strings.ToLower(lcard)]
	rc, isRightKnown := cardAny[strings.ToLower(rcard)]
	if e1 == "" || e2 == "" || !isLeftKnown || !isRightKnown {
		return false
	}
	self.entity(e1)
	self.entity(e2)
	self.Relationships = append(self.Relationships, &Relationship{
		Left: e1, Right: e2, LeftCard: lc, RightCard: rc,
		Identifying: isIdentifying, Label: label,
	})
	return true
}

func findWordOp(s string) (int, string) {
	for _, w := range []string{" optionally to ", " to "} {
		if i := strings.Index(s, w); i >= 0 {
			return i, w
		}
	}
	return -1, ""
}

func splitEntityCard(part string, isEntityFirst bool) (string, string) {
	part = strings.TrimSpace(part)
	if isEntityFirst {
		if strings.HasPrefix(part, `"`) {
			if end := strings.Index(part[1:], `"`); end >= 0 {
				return part[1 : end+1], strings.TrimSpace(part[end+2:])
			}
		}
		toks := strings.Fields(part)
		if len(toks) == 0 {
			return "", ""
		}
		return strings.Trim(toks[0], `"`), strings.Join(toks[1:], " ")
	}
	if strings.HasSuffix(part, `"`) {
		if start := strings.LastIndex(part[:len(part)-1], `"`); start >= 0 {
			return part[start+1 : len(part)-1], strings.TrimSpace(part[:start])
		}
	}
	toks := strings.Fields(part)
	if len(toks) == 0 {
		return "", ""
	}
	return strings.Trim(toks[len(toks)-1], `"`), strings.Join(toks[:len(toks)-1], " ")
}

func splitAttrTokens(text string) []string {
	var toks []string
	var cur strings.Builder
	depth := 0
	isInTick := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, letter := range text {
		switch {
		case letter == '`':
			isInTick = !isInTick
			cur.WriteRune(letter)
		case letter == '(' && !isInTick:
			depth++
			cur.WriteRune(letter)
		case letter == ')' && !isInTick:
			if depth > 0 {
				depth--
			}
			cur.WriteRune(letter)
		case (letter == ' ' || letter == '\t') && depth == 0 && !isInTick:
			flush()
		default:
			cur.WriteRune(letter)
		}
	}
	flush()
	return toks
}
