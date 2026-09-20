package mermaid

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/mermaid/orderedmap"
)

type graphProperties struct {
	data             *orderedmap.OrderedMap[string, []textEdge]
	nodeSpecs        map[string]graphNodeSpec
	styleClasses     *map[string]styleClass
	boxBorderPadding int
	graphDirection   string
	styleType        string
	paddingX         int
	paddingY         int
	subgraphs        []*textSubgraph
	useAscii         bool
}

type textNode struct {
	name       string
	label      graphLabel
	hasLabel   bool
	styleClass string
}

type graphNodeSpec struct {
	label           graphLabel
	labelIsExplicit bool
	styleClass      string
}

type textEdge struct {
	parent          textNode
	child           textNode
	label           string
	isBidirectional bool
}

type textSubgraph struct {
	id       string
	name     string
	label    graphLabel
	nodes    []string
	parent   *textSubgraph
	children []*textSubgraph
}

func parseSubgraphHeader(header string) textSubgraph {
	trimmedHeader := strings.TrimSpace(header)
	labelText := trimmedHeader
	id := ""

	if match := regexp.MustCompile(`^(\S+)\s*\[(.+)\]$`).FindStringSubmatch(trimmedHeader); match != nil {
		id = strings.TrimSpace(match[1])
		labelText = strings.TrimSpace(match[2])
		labelText = strings.Trim(labelText, `"`)
	}

	return textSubgraph{
		id:    id,
		name:  labelText,
		label: newGraphLabel(labelText),
		nodes: []string{},
	}
}

func splitGraphLines(mermaid string) []string {
	lines := []string{}
	var current strings.Builder
	bracketDepth := 0
	isInQuotes := false

	for i := 0; i < len(mermaid); i++ {
		switch mermaid[i] {
		case '"':
			isInQuotes = !isInQuotes
		case '[':
			if !isInQuotes {
				bracketDepth++
			}
		case ']':
			if !isInQuotes && bracketDepth > 0 {
				bracketDepth--
			}
		case '\n':
			if bracketDepth == 0 {
				lines = append(lines, current.String())
				current.Reset()
				continue
			}
		case '\\':
			if i+1 < len(mermaid) && mermaid[i+1] == 'n' && bracketDepth == 0 {
				lines = append(lines, current.String())
				current.Reset()
				i++
				continue
			}
		}

		current.WriteByte(mermaid[i])
	}

	return append(lines, current.String())
}

var (
	nodeNamePattern   = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}_.:-]*$`)
	styleClassPattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)
)

func parseNode(line string) (textNode, error) {
	trimmedLine := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ";"))
	styleClass := ""
	if index := strings.LastIndex(trimmedLine, ":::"); index != -1 {
		styleClass = strings.TrimSpace(trimmedLine[index+3:])
		trimmedLine = strings.TrimSpace(trimmedLine[:index])
		if !styleClassPattern.MatchString(styleClass) {
			return textNode{}, fmt.Errorf("invalid style class %q", styleClass)
		}
	}

	name := trimmedLine
	labelText := trimmedLine
	hasLabel := false
	if open := strings.Index(trimmedLine, "["); open != -1 {
		if open == 0 || !strings.HasSuffix(trimmedLine, "]") {
			return textNode{}, fmt.Errorf("invalid node %q", line)
		}
		name = strings.TrimSpace(trimmedLine[:open])
		labelText = strings.TrimSpace(trimmedLine[open+1 : len(trimmedLine)-1])
		labelText = strings.Trim(labelText, `"`)
		hasLabel = true
	}
	if !nodeNamePattern.MatchString(name) {
		return textNode{}, fmt.Errorf("invalid node name %q", name)
	}

	return textNode{
		name:       name,
		label:      newGraphLabel(labelText),
		hasLabel:   hasLabel,
		styleClass: styleClass,
	}, nil
}

func (self *graphProperties) parseExpression(expression string) ([]textNode, error) {
	nodes, err := self.parseString(expression)
	if err == nil {
		return nodes, nil
	}
	node, nodeError := parseNode(expression)
	if nodeError != nil {
		return nil, nodeError
	}
	return []textNode{node}, nil
}

func parseStyleClass(matchedLine []string) (styleClass, error) {
	className := strings.TrimSpace(matchedLine[0])
	if !styleClassPattern.MatchString(className) {
		return styleClass{}, fmt.Errorf("invalid style class %q", className)
	}

	styleMap := make(map[string]string)
	for declaration := range strings.SplitSeq(matchedLine[1], ",") {
		property, value, ok := strings.Cut(declaration, ":")
		if !ok || strings.TrimSpace(property) == "" || strings.TrimSpace(value) == "" {
			return styleClass{}, fmt.Errorf("invalid style declaration %q", declaration)
		}
		styleMap[strings.TrimSpace(property)] = strings.TrimSpace(value)
	}
	return styleClass{className, styleMap}, nil
}

func setArrowWithLabel(lhs []textNode, rhs []textNode, label string, isBidirectional bool, gp *graphProperties) []textNode {
	for _, l := range lhs {
		for _, r := range rhs {
			setData(l, textEdge{l, r, label, isBidirectional}, gp.data, gp.nodeSpecs)
		}
	}
	return rhs
}

func setArrow(lhs []textNode, rhs []textNode, gp *graphProperties) []textNode {
	return setArrowWithLabel(lhs, rhs, "", false, gp)
}

func setBidirectionalArrow(lhs []textNode, rhs []textNode, gp *graphProperties) []textNode {
	return setArrowWithLabel(lhs, rhs, "", true, gp)
}

func rememberNode(node textNode, nodeSpecs map[string]graphNodeSpec) {
	spec := nodeSpecs[node.name]
	if node.hasLabel || len(spec.label.lines) == 0 {
		spec.label = node.label
		spec.labelIsExplicit = node.hasLabel
	}
	if node.styleClass != "" {
		spec.styleClass = node.styleClass
	}
	nodeSpecs[node.name] = spec
}

func addNode(node textNode, data *orderedmap.OrderedMap[string, []textEdge], nodeSpecs map[string]graphNodeSpec) {
	rememberNode(node, nodeSpecs)
	if _, ok := data.Get(node.name); !ok {
		data.Set(node.name, []textEdge{})
	}
}

func setData(parent textNode, edge textEdge, data *orderedmap.OrderedMap[string, []textEdge], nodeSpecs map[string]graphNodeSpec) {
	rememberNode(parent, nodeSpecs)
	rememberNode(edge.child, nodeSpecs)
	if children, ok := data.Get(parent.name); ok {
		data.Set(parent.name, append(children, edge))
	} else {
		data.Set(parent.name, []textEdge{edge})
	}
	if _, ok := data.Get(edge.child.name); ok {
	} else {
		data.Set(edge.child.name, []textEdge{})
	}
}

func (self *graphProperties) parseString(line string) ([]textNode, error) {
	patterns := []struct {
		regex   *regexp.Regexp
		handler func([]string) ([]textNode, error)
	}{
		{
			regex: regexp.MustCompile(`^\s*$`),
			handler: func(_ []string) ([]textNode, error) {
				return []textNode{}, nil
			},
		},
		{
			regex: regexp.MustCompile(`(?s)^(.+)\s*<-->\s*\|(.+)\|\s*(.+)$`),
			handler: func(match []string) ([]textNode, error) {
				left, err := self.parseExpression(match[0])
				if err != nil {
					return nil, err
				}
				right, err := self.parseExpression(match[2])
				if err != nil {
					return nil, err
				}
				return setArrowWithLabel(left, right, match[1], true, self), nil
			},
		},
		{
			regex: regexp.MustCompile(`(?s)^(.+)\s*<-->\s*(.+)$`),
			handler: func(match []string) ([]textNode, error) {
				left, err := self.parseExpression(match[0])
				if err != nil {
					return nil, err
				}
				right, err := self.parseExpression(match[1])
				if err != nil {
					return nil, err
				}
				return setBidirectionalArrow(left, right, self), nil
			},
		},
		{
			regex: regexp.MustCompile(`(?s)^(.+)\s*-->\s*\|(.+)\|\s*(.+)$`),
			handler: func(match []string) ([]textNode, error) {
				left, err := self.parseExpression(match[0])
				if err != nil {
					return nil, err
				}
				right, err := self.parseExpression(match[2])
				if err != nil {
					return nil, err
				}
				return setArrowWithLabel(left, right, match[1], false, self), nil
			},
		},
		{
			regex: regexp.MustCompile(`(?s)^(.+)\s*-->\s*(.+)$`),
			handler: func(match []string) ([]textNode, error) {
				left, err := self.parseExpression(match[0])
				if err != nil {
					return nil, err
				}
				right, err := self.parseExpression(match[1])
				if err != nil {
					return nil, err
				}
				return setArrow(left, right, self), nil
			},
		},
		{
			regex: regexp.MustCompile(`^classDef\s+(.+)\s+(.+)$`),
			handler: func(match []string) ([]textNode, error) {
				style, err := parseStyleClass(match)
				if err != nil {
					return nil, err
				}
				(*self.styleClasses)[style.name] = style
				return []textNode{}, nil
			},
		},
		{
			regex: regexp.MustCompile(`(?s)^(.+) & (.+)$`),
			handler: func(match []string) ([]textNode, error) {
				left, err := self.parseExpression(match[0])
				if err != nil {
					return nil, err
				}
				right, err := self.parseExpression(match[1])
				if err != nil {
					return nil, err
				}
				return append(left, right...), nil
			},
		},
	}
	for _, pattern := range patterns {
		if match := pattern.regex.FindStringSubmatch(line); match != nil {
			return pattern.handler(match[1:])
		}
	}
	return nil, errors.New("could not parse line: " + line)
}

func mermaidFileToMap(mermaid string) (*graphProperties, error) {
	rawLines := splitGraphLines(mermaid)

	lines := []string{}
	for _, line := range rawLines {
		if line == "---" {
			break
		}

		if strings.HasPrefix(strings.TrimSpace(line), "%%") {
			continue
		}

		if index := strings.Index(line, "%%"); index != -1 {
			line = strings.TrimSpace(line[:index])
		}

		if len(strings.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}

	data := orderedmap.NewOrderedMap[string, []textEdge]()
	styleClasses := make(map[string]styleClass)
	properties := graphProperties{
		data:             data,
		nodeSpecs:        make(map[string]graphNodeSpec),
		styleClasses:     &styleClasses,
		boxBorderPadding: boxBorderPadding,
		graphDirection:   "",
		styleType:        "cli",
		paddingX:         paddingBetweenX,
		paddingY:         paddingBetweenY,
		subgraphs:        []*textSubgraph{},
	}

	paddingRegex := regexp.MustCompile(`^(?i)padding([xy])\s*=\s*(\d+)$`)
	for len(lines) > 0 {
		trimmedLine := strings.TrimSpace(lines[0])
		if match := paddingRegex.FindStringSubmatch(trimmedLine); match != nil {
			paddingValue, err := strconv.Atoi(match[2])
			if err != nil {
				return &properties, err
			}
			if strings.EqualFold(match[1], "x") {
				properties.paddingX = paddingValue
			} else {
				properties.paddingY = paddingValue
			}
			lines = lines[1:]
			continue
		}
		break
	}

	if len(lines) == 0 {
		return &properties, errors.New("missing graph definition")
	}

	fields := strings.Fields(strings.TrimRight(lines[0], "; \t\r"))
	if len(fields) == 0 || (fields[0] != "graph" && fields[0] != "flowchart") {
		return &properties, fmt.Errorf("unsupported graph type '%s'. Supported types: 'graph' or 'flowchart' with an optional direction (TD, TB, BT, LR, RL)", strings.TrimSpace(lines[0]))
	}
	if len(fields) > 2 {
		return &properties, fmt.Errorf("unexpected tokens after graph direction: %q", strings.Join(fields[2:], " "))
	}

	properties.graphDirection = "TD"
	if len(fields) == 2 {
		switch fields[1] {
		case "LR", "RL":
			properties.graphDirection = "LR"
		case "TD", "TB", "BT":
			properties.graphDirection = "TD"
		default:
			return &properties, fmt.Errorf("unsupported graph direction '%s'. Supported directions: TD, TB, BT, LR, RL", fields[1])
		}
	}
	lines = lines[1:]

	subgraphStack := []*textSubgraph{}
	subgraphRegex := regexp.MustCompile(`^\s*subgraph\s+(.+)$`)
	endRegex := regexp.MustCompile(`^\s*end\s*$`)

	for lineIndex, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if match := subgraphRegex.FindStringSubmatch(trimmedLine); match != nil {
			header := parseSubgraphHeader(match[1])
			newSubgraph := &textSubgraph{
				id:       header.id,
				name:     header.name,
				label:    header.label,
				nodes:    []string{},
				children: []*textSubgraph{},
			}

			if len(subgraphStack) > 0 {
				parent := subgraphStack[len(subgraphStack)-1]
				newSubgraph.parent = parent
				parent.children = append(parent.children, newSubgraph)
			}

			subgraphStack = append(subgraphStack, newSubgraph)
			properties.subgraphs = append(properties.subgraphs, newSubgraph)
			continue
		}

		if endRegex.MatchString(trimmedLine) {
			if len(subgraphStack) > 0 {
				subgraphStack = subgraphStack[:len(subgraphStack)-1]
			}
			continue
		}

		existingNodes := make(map[string]bool)
		for el := data.Front(); el != nil; el = el.Next() {
			existingNodes[el.Key] = true
		}

		nodes, err := properties.parseExpression(line)
		if err != nil {
			return &properties, fmt.Errorf("line %d: %w", lineIndex+2, err)
		}
		for _, node := range nodes {
			addNode(node, properties.data, properties.nodeSpecs)
		}

		if len(subgraphStack) > 0 {
			for el := data.Front(); el != nil; el = el.Next() {
				nodeName := el.Key
				if !existingNodes[nodeName] {
					for _, sg := range subgraphStack {
						found := slices.Contains(sg.nodes, nodeName)
						if !found {
							sg.nodes = append(sg.nodes, nodeName)
						}
					}
				}
			}
		}
	}
	if len(subgraphStack) > 0 {
		return &properties, fmt.Errorf("unclosed subgraph: missing %d end", len(subgraphStack))
	}
	return &properties, nil
}
