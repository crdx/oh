package markdown

import (
	"strconv"
	"strings"

	"crdx.org/col"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"crdx.org/io/internal/app/link"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/mermaid"
)

const tab = "    "

const mermaidLanguage = "mermaid"

var markdownParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

type PictureDrawer interface {
	DrawPicture(path string, columns int) ([]string, bool)
}

type Options struct {
	Columns                int
	ShouldRenderHyperlinks bool
	LinkRoot               link.Roots
	Pictures               PictureDrawer
}

func Render(markdown string, columns int) []string {
	return render(markdown, Options{Columns: columns}, nil)
}

func RenderWithHyperlinks(markdown string, columns int) []string {
	return render(markdown, Options{Columns: columns, ShouldRenderHyperlinks: true}, nil)
}

func RenderWithHyperlinksUnder(markdown string, columns int, linkRoot link.Roots) []string {
	return render(markdown, Options{
		Columns:                columns,
		ShouldRenderHyperlinks: true,
		LinkRoot:               linkRoot,
	}, nil)
}

func RenderWith(markdown string, options Options) []string {
	return render(markdown, options, nil)
}

func EndsWithTable(markdown string) bool {
	source := []byte(strings.ReplaceAll(markdown, "\t", tab))
	document := markdownParser.Parse(text.NewReader(source))

	_, isTable := document.LastChild().(*extensionast.Table)

	return isTable
}

type StreamRenderer struct {
	mermaidRows             map[int][]string
	isTailMermaid           bool
	hasMermaid              bool
	hasLinkReference        bool
	stableCandidateStart    int
	hasStableCandidateStart bool
}

func (self *StreamRenderer) Render(markdown string, columns int) []string {
	return self.render(markdown, Options{Columns: columns})
}

func (self *StreamRenderer) IsTailMermaid() bool {
	return self.isTailMermaid
}

func (self *StreamRenderer) Reset() {
	clear(self.mermaidRows)
	self.isTailMermaid = false
	self.hasMermaid = false
	self.hasLinkReference = false
	self.stableCandidateStart = 0
	self.hasStableCandidateStart = false
}

func (self *StreamRenderer) render(markdown string, options Options) []string {
	self.isTailMermaid = false
	self.hasMermaid = false
	self.hasLinkReference = false
	self.stableCandidateStart = 0
	self.hasStableCandidateStart = false

	return render(markdown, options, self)
}

func render(markdown string, options Options, stream *StreamRenderer) []string {
	source := []byte(strings.ReplaceAll(markdown, "\t", tab))
	parserContext := parser.NewContext()
	document := markdownParser.Parse(text.NewReader(source), parser.WithContext(parserContext))
	if stream != nil {
		stream.hasLinkReference = len(parserContext.References()) > 0
		if lastBlock := document.LastChild(); lastBlock != nil {
			if candidate := lastBlock.PreviousSibling(); candidate != nil && candidate.Pos() >= 0 {
				stream.stableCandidateStart = originalOffset(markdown, candidate.Pos())
				stream.hasStableCandidateStart = true
			}
		}
	}

	mermaidBlock := 0
	renderer := &renderer{
		source:                 source,
		columns:                options.Columns,
		mermaidBlock:           &mermaidBlock,
		stream:                 stream,
		shouldRenderHyperlinks: options.ShouldRenderHyperlinks,
		linkRoot:               options.LinkRoot,
		pictures:               options.Pictures,
	}
	renderer.blocks(document)

	return renderer.rows
}

func originalOffset(markdown string, expandedOffset int) int {
	expandedAt := 0
	for originalAt := range len(markdown) {
		if expandedAt >= expandedOffset {
			return originalAt
		}
		if markdown[originalAt] == '\t' {
			expandedAt += len(tab)
		} else {
			expandedAt++
		}
	}
	return len(markdown)
}

type renderer struct {
	source                 []byte
	columns                int
	mermaidBlock           *int
	isTight                bool
	rows                   []string
	stream                 *StreamRenderer
	shouldRenderHyperlinks bool
	linkRoot               link.Roots
	pictures               PictureDrawer
}

func (self *renderer) blocks(parent ast.Node) {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		if len(self.rows) > 0 && !self.isTight {
			self.rows = append(self.rows, "")
		}

		self.block(node)
	}

	for len(self.rows) > 0 && self.rows[len(self.rows)-1] == "" {
		self.rows = self.rows[:len(self.rows)-1]
	}
}

func (self *renderer) block(node ast.Node) {
	if self.stream != nil {
		self.stream.isTailMermaid = false
	}

	switch node := node.(type) {
	case *ast.Heading:
		self.appendWrapped(over(col.Bold, style.Heading(self.inline(node))))

	case *ast.FencedCodeBlock:
		language := string(node.Language(self.source))
		lines := self.lines(node)
		if language == mermaidLanguage {
			if self.stream != nil {
				self.stream.hasMermaid = true
			}

			block := *self.mermaidBlock
			*self.mermaidBlock++
			if self.mermaid(lines, block) {
				if self.stream != nil {
					self.stream.isTailMermaid = true
				}

				return
			}
		}
		self.code(emphasise(lines, language))

	case *ast.CodeBlock:
		self.code(emphasise(self.lines(node), ""))

	case *ast.HTMLBlock:
		self.code(emphasise(self.lines(node), ""))

	case *ast.ThematicBreak:
		self.rows = append(self.rows, style.Border(strings.Repeat("─", max(self.columns, 0))))

	case *ast.Blockquote:
		self.quote(node)

	case *ast.List:
		self.list(node)

	case *extensionast.Table:
		self.rows = append(self.rows, self.table(node)...)

	case *ast.Paragraph, *ast.TextBlock:
		if self.picture(node) {
			return
		}

		self.appendWrapped(self.inline(node))

	default:
		self.appendWrapped(self.inline(node))
	}
}

func (self *renderer) appendWrapped(styledText string) {
	self.rows = append(self.rows, width.Wrap(self.linkPaths(styledText), self.columns)...)
}

func (self *renderer) linkPaths(text string) string {
	if !self.shouldRenderHyperlinks || self.linkRoot.IsEmpty() {
		return text
	}

	return link.Render(text, self.linkRoot)
}

func (self *renderer) code(lines []string) {
	for _, line := range lines {
		if strings.TrimSpace(style.Plain(line)) == "" {
			self.rows = append(self.rows, "")
			continue
		}

		self.rows = append(self.rows, width.Wrap(self.linkPaths(line), self.columns)...)
	}
}

func (self *renderer) mermaid(lines []string, block int) bool {
	if rows, isDrawable := renderMermaidRows(lines); isDrawable {
		if neededColumns := widestRow(rows); neededColumns > self.columns {
			self.forgetMermaidRows(block)
			self.appendWrapped(over(col.Italic, style.Subtle(diagramWidthNotice(neededColumns))))
			return false
		}

		self.rows = append(self.rows, rows...)
		self.rememberMermaidRows(block, rows)
		return true
	}

	if self.stream == nil {
		return false
	}
	cachedRows, hasCachedRows := self.stream.mermaidRows[block]
	if !hasCachedRows || widestRow(cachedRows) > self.columns {
		return false
	}
	self.rows = append(self.rows, cachedRows...)
	return true
}

func diagramWidthNotice(neededColumns int) string {
	return "Diagram needs " + strconv.Itoa(neededColumns) + " columns."
}

func renderMermaidRows(lines []string) ([]string, bool) {
	diagram, err := mermaid.Render(strings.Join(lines, "\n"))
	if err != nil || diagram == "" {
		return nil, false
	}

	return strings.Split(diagram, "\n"), true
}

func (self *renderer) rememberMermaidRows(block int, rows []string) {
	if self.stream == nil {
		return
	}

	if self.stream.mermaidRows == nil {
		self.stream.mermaidRows = map[int][]string{}
	}

	self.stream.mermaidRows[block] = rows
}

func (self *renderer) forgetMermaidRows(block int) {
	if self.stream == nil {
		return
	}

	delete(self.stream.mermaidRows, block)
}

func widestRow(rows []string) int {
	widest := 0
	for _, row := range rows {
		widest = max(widest, width.Of(row))
	}

	return widest
}

func (self *renderer) quote(node ast.Node) {
	lead, room := margin(self.columns, "│ ")

	inner := &renderer{
		source:                 self.source,
		columns:                room,
		mermaidBlock:           self.mermaidBlock,
		stream:                 self.stream,
		shouldRenderHyperlinks: self.shouldRenderHyperlinks,
		linkRoot:               self.linkRoot,
		pictures:               self.pictures,
	}
	inner.blocks(node)

	for _, row := range inner.rows {
		self.rows = append(self.rows, style.Border(lead)+over(style.Quote, row))
	}
}

func (self *renderer) list(node *ast.List) {
	number := node.Start

	for item := node.FirstChild(); item != nil; item = item.NextSibling() {
		marker := "• "

		if node.IsOrdered() {
			marker = strconv.Itoa(number) + ". "
			number++
		}

		self.item(marker, item)
	}
}

func (self *renderer) item(marker string, node ast.Node) {
	if self.stream != nil {
		self.stream.isTailMermaid = false
	}

	room := self.columns - width.Of(marker)
	if room < 1 {
		self.appendWrapped(style.Bullet(marker) + self.inline(node))
		return
	}

	inner := &renderer{
		source:                 self.source,
		columns:                room,
		mermaidBlock:           self.mermaidBlock,
		isTight:                true,
		stream:                 self.stream,
		shouldRenderHyperlinks: self.shouldRenderHyperlinks,
		linkRoot:               self.linkRoot,
		pictures:               self.pictures,
	}
	inner.blocks(node)

	hangingIndent := strings.Repeat(" ", width.Of(marker))

	for i, row := range inner.rows {
		if i == 0 {
			self.rows = append(self.rows, style.Bullet(marker)+row)
			continue
		}

		self.rows = append(self.rows, hangingIndent+row)
	}
}

func (self *renderer) lines(node ast.Node) []string {
	segments := node.Lines()
	lines := make([]string, 0, segments.Len())

	for i := range segments.Len() {
		segment := segments.At(i)
		lines = append(lines, strings.TrimRight(string(segment.Value(self.source)), "\n"))
	}

	return lines
}

func margin(cells int, prefix string) (string, int) {
	if prefixWidth := width.Of(prefix); cells > prefixWidth {
		return prefix, cells - prefixWidth
	}

	return "", cells
}
