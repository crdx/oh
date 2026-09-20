package markdown

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"mvdan.cc/sh/v3/syntax"

	"crdx.org/io/internal/app/style"
)

func Emphasise(line string, language string) string {
	return Highlight(line, line, language, false)
}

func HighlightText(text string, language string) string {
	return strings.Join(emphasise(strings.Split(text, "\n"), language), "\n")
}

func HighlightFile(path string, text string) string {
	lexer := lexers.Match(path)
	if lexer == nil {
		return HighlightText(text, "")
	}
	return HighlightText(text, lexer.Config().Name)
}

var lexerCache = struct {
	sync.Mutex

	entries map[string]chroma.Lexer
}{entries: map[string]chroma.Lexer{}}

func lexerFor(language string) chroma.Lexer {
	lexerCache.Lock()
	defer lexerCache.Unlock()

	if lexer, ok := lexerCache.entries[language]; ok {
		return lexer
	}

	lexer := lexers.Get(language)
	lexerCache.entries[language] = lexer
	return lexer
}

func emphasise(lines []string, language string) []string {
	lexer := lexerFor(language)
	if language == "" || lexer == nil {
		return plainly(lines)
	}

	iterator, err := lexer.Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		return plainly(lines)
	}

	rows := []string{""}

	for _, token := range iterator.Tokens() {
		style := tokenStyle(token.Type)

		for i, piece := range strings.Split(token.Value, "\n") {
			if i > 0 {
				rows = append(rows, "")
			}

			switch {
			case piece == "":
			case strings.TrimSpace(piece) == "":
				rows[len(rows)-1] += piece
			default:
				rows[len(rows)-1] += style(piece)
			}
		}
	}

	return rows[:min(len(rows), len(lines))]
}

func Highlight(source string, target string, language string, isElided bool) string {
	switch language {
	case "bash":
		return highlightBash(source, target, isElided)
	case "regexp":
		return highlightRegExp(source, target, isElided)
	default:
		if isElided {
			target += "…"
		}
		return emphasise([]string{target}, language)[0]
	}
}

type sourceSpan struct {
	start int
	end   int
	style style.Style
}

func highlightBash(source string, target string, wasElided bool) string {
	spans, err := bashCommandSpans(source)
	if err != nil {
		if wasElided {
			target += "…"
		}
		return style.Block(target)
	}

	return paintSpans(target, spans, wasElided)
}

func paintSpans(target string, spans []sourceSpan, wasElided bool) string {
	boundary := len(target)
	var output strings.Builder
	position := 0

	for _, span := range spans {
		if span.start >= boundary {
			break
		}
		if span.end <= position {
			continue
		}

		if position < span.start {
			output.WriteString(style.Block(target[position:span.start]))
		}

		end := min(span.end, boundary)
		output.WriteString(span.style(target[max(position, span.start):end]))
		position = end
	}

	if position < boundary {
		output.WriteString(style.Block(target[position:]))
	}

	if wasElided {
		style := style.Block
		for _, span := range spans {
			if span.start <= boundary && boundary < span.end {
				style = span.style
				break
			}
		}
		output.WriteString(style("…"))
	}

	return output.String()
}

func bashCommandSpans(source string) ([]sourceSpan, error) {
	parsedScript, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(source), "")
	if err != nil {
		return nil, err
	}

	var spans []sourceSpan
	syntax.Walk(parsedScript, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			for _, word := range node.Args[:min(2, len(node.Args))] {
				spans = append(spans, sourceSpan{
					start: int(word.Pos().Offset()),
					end:   int(word.End().Offset()),
					style: style.Function,
				})
			}
		case *syntax.Assign:
			spans = append(spans, bashAssignmentSpans(node)...)
		case *syntax.Redirect:
			spans = append(spans, bashSourceSpan(node.OpPos, node.Op.String(), style.Operator))
		case *syntax.BinaryCmd:
			spans = append(spans, bashSourceSpan(node.OpPos, node.Op.String(), style.Operator))
		case *syntax.Stmt:
			if node.Semicolon.IsValid() {
				spans = append(spans, bashSourceSpan(node.Semicolon, ";", style.Operator))
			}
		case *syntax.ForClause:
			keyword := "for"
			if node.Select {
				keyword = "select"
			}
			spans = append(spans, bashSourceSpan(node.ForPos, keyword, style.Keyword))
			spans = append(spans, bashLoopBodySpans(node.DoPos, node.DonePos)...)
		case *syntax.WhileClause:
			keyword := "while"
			if node.Until {
				keyword = "until"
			}
			spans = append(spans, bashSourceSpan(node.WhilePos, keyword, style.Keyword))
			spans = append(spans, bashLoopBodySpans(node.DoPos, node.DonePos)...)
		case *syntax.IfClause:
			if node.Position.IsValid() {
				spans = append(spans, bashKeywordSpan(source, node.Position))
			}
			if node.ThenPos.IsValid() {
				spans = append(spans, bashSourceSpan(node.ThenPos, "then", style.Keyword))
			}
			if node.FiPos.IsValid() {
				spans = append(spans, bashSourceSpan(node.FiPos, "fi", style.Keyword))
			}
		case *syntax.CaseClause:
			spans = append(spans, bashSourceSpan(node.Case, "case", style.Keyword))
			if node.In.IsValid() {
				spans = append(spans, bashSourceSpan(node.In, "in", style.Keyword))
			}
			if node.Esac.IsValid() {
				spans = append(spans, bashSourceSpan(node.Esac, "esac", style.Keyword))
			}
		case *syntax.CaseItem:
			if node.OpPos.IsValid() {
				spans = append(spans, bashSourceSpan(node.OpPos, node.Op.String(), style.Operator))
			}
		case *syntax.WordIter:
			if node.InPos.IsValid() {
				spans = append(spans, bashSourceSpan(node.InPos, "in", style.Keyword))
			}
		}
		return true
	})

	slices.SortFunc(spans, bySourcePosition)

	return spans, nil
}

func bySourcePosition(first sourceSpan, second sourceSpan) int {
	if first.start == second.start {
		return cmp.Compare(first.end, second.end)
	}

	return cmp.Compare(first.start, second.start)
}

func bashSourceSpan(position syntax.Pos, text string, style style.Style) sourceSpan {
	start := int(position.Offset())
	return sourceSpan{start: start, end: start + len(text), style: style}
}

func bashKeywordSpan(source string, position syntax.Pos) sourceSpan {
	start := int(position.Offset())

	end := start
	for end < len(source) && source[end] >= 'a' && source[end] <= 'z' {
		end++
	}

	return sourceSpan{start: start, end: end, style: style.Keyword}
}

func bashLoopBodySpans(doPosition syntax.Pos, donePosition syntax.Pos) []sourceSpan {
	var spans []sourceSpan

	if doPosition.IsValid() {
		spans = append(spans, bashSourceSpan(doPosition, "do", style.Keyword))
	}

	if donePosition.IsValid() {
		spans = append(spans, bashSourceSpan(donePosition, "done", style.Keyword))
	}

	return spans
}

func bashAssignmentSpans(assignment *syntax.Assign) []sourceSpan {
	if assignment.Name == nil {
		return nil
	}

	nameEnd := int(assignment.Name.End().Offset())
	spans := []sourceSpan{{
		start: int(assignment.Name.Pos().Offset()),
		end:   nameEnd,
		style: style.Block,
	}}

	valueStart := int(assignment.End().Offset())
	if assignment.Value != nil {
		valueStart = int(assignment.Value.Pos().Offset())
	}
	spans = append(spans, sourceSpan{start: nameEnd, end: valueStart, style: style.Operator})

	if assignment.Value != nil {
		spans = append(spans, sourceSpan{
			start: valueStart,
			end:   int(assignment.Value.End().Offset()),
			style: style.Block,
		})
	}

	return spans
}

type regexpStyle int

const (
	regexpLiteral regexpStyle = iota
	regexpKeyword
	regexpOperator
	regexpPunctuation
)

type styledSpan struct {
	start int
	end   int
	style regexpStyle
}

func (self regexpStyle) paint() style.Style {
	switch self {
	case regexpKeyword:
		return style.Keyword
	case regexpOperator:
		return style.Operator
	case regexpLiteral, regexpPunctuation:
		return style.Block
	default:
		return style.Block
	}
}

func highlightRegExp(source string, retainedPrefix string, isElided bool) string {
	spans := regexpSpans(source)
	boundary := len(retainedPrefix)
	var output strings.Builder

	for _, span := range spans {
		if span.start >= boundary {
			break
		}

		output.WriteString(span.style.paint()(retainedPrefix[span.start:min(span.end, boundary)]))
	}

	if isElided {
		style := regexpLiteral
		for _, span := range spans {
			if span.start <= boundary && boundary < span.end {
				style = span.style
				break
			}
		}
		output.WriteString(style.paint()("…"))
	}

	return output.String()
}

func regexpSpans(source string) []styledSpan {
	spans := make([]styledSpan, 0, len(source))
	isInCharacterClass := false

	for start := 0; start < len(source); {
		end := start + 1
		style := regexpLiteral

		switch source[start] {
		case '\\':
			end = regexpEscapeEnd(source, start)
			style = regexpKeyword
		case '[':
			isInCharacterClass = true
			style = regexpPunctuation
		case ']':
			isInCharacterClass = false
			style = regexpPunctuation
		case '-', '&':
			if isInCharacterClass {
				style = regexpOperator
			}
		case '^', '$':
			if !isInCharacterClass {
				style = regexpKeyword
			}
		case '(', ')':
			style = regexpPunctuation
		case '.', '*', '+', '?', '|':
			if !isInCharacterClass {
				style = regexpOperator
			}
		case '{':
			if !isInCharacterClass {
				end = regexpRepetitionEnd(source, start)
				style = regexpOperator
			}
		default:
			_, size := utf8.DecodeRuneInString(source[start:])
			end = start + size
		}

		if len(spans) > 0 && spans[len(spans)-1].end == start && spans[len(spans)-1].style == style {
			spans[len(spans)-1].end = end
		} else {
			spans = append(spans, styledSpan{start: start, end: end, style: style})
		}
		start = end
	}

	return spans
}

func regexpEscapeEnd(source string, start int) int {
	if start+1 >= len(source) {
		return start + 1
	}

	_, size := utf8.DecodeRuneInString(source[start+1:])
	end := start + 1 + size
	if (source[start+1] == 'p' || source[start+1] == 'P') && end < len(source) && source[end] == '{' {
		if closeAt := strings.IndexByte(source[end+1:], '}'); closeAt >= 0 {
			return end + closeAt + 2
		}
	}

	return end
}

func regexpRepetitionEnd(source string, start int) int {
	if closeAt := strings.IndexByte(source[start+1:], '}'); closeAt >= 0 {
		return start + closeAt + 2
	}

	return start + 1
}

func plainly(lines []string) []string {
	highlightedLines := make([]string, len(lines))

	for i, line := range lines {
		highlightedLines[i] = style.Block(line)
	}

	return highlightedLines
}

func tokenStyle(token chroma.TokenType) style.Style {
	switch token.Category() { //nolint:exhaustive // Category collapses tokens into top-level groups.
	case chroma.Comment:
		return style.Comment
	case chroma.Keyword:
		return keyword(token)
	case chroma.Literal:
		return literal(token)
	case chroma.Name:
		return name(token)
	case chroma.Operator:
		return style.Operator
	case chroma.Punctuation:
		return style.Punctuation
	case chroma.Generic:
		return generic(token)
	case chroma.Text, chroma.Error, chroma.Other:
		return style.Block
	}

	return style.Block
}

func generic(token chroma.TokenType) style.Style {
	switch token { //nolint:exhaustive // Unlisted generic tokens deliberately use the plain style.
	case chroma.GenericInserted:
		return style.InsertedText
	case chroma.GenericDeleted:
		return style.DeletedText
	case chroma.GenericHeading, chroma.GenericSubheading:
		return style.Hunk
	}

	return style.Block
}

func keyword(token chroma.TokenType) style.Style {
	if token == chroma.KeywordType {
		return style.Type
	}

	return style.Keyword
}

func literal(token chroma.TokenType) style.Style {
	if token.SubCategory() == chroma.LiteralNumber {
		return style.Number
	}

	return style.Literal
}

func name(token chroma.TokenType) style.Style {
	switch token { //nolint:exhaustive // Unlisted name tokens deliberately use the variable style.
	case chroma.NameFunction, chroma.NameFunctionMagic:
		return style.Function
	case chroma.NameClass, chroma.NameNamespace, chroma.NameBuiltin:
		return style.Type
	}

	return style.Variable
}
