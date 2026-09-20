package markdown

import (
	"strings"

	"crdx.org/col"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/style"
)

const reset = "\x1b[0m"

func (self *renderer) inline(parent ast.Node) string {
	var out strings.Builder

	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		out.WriteString(self.renderInlineNode(node))
	}

	return out.String()
}

func (self *renderer) renderInlineNode(node ast.Node) string {
	switch node := node.(type) {
	case *ast.Text:
		return style.Answer(self.text(node)) + lineBreak(node)

	case *ast.String:
		return style.Answer(string(node.Value))

	case *ast.CodeSpan:
		return style.Code(self.words(node))

	case *ast.Emphasis:
		if node.Level >= 2 {
			return over(col.Bold, self.inline(node))
		}

		return over(col.Italic, self.inline(node))

	case *extensionast.Strikethrough:
		return over(col.Strikethrough, self.inline(node))

	case *ast.Link:
		address := string(node.Destination)
		label := self.hyperlink(style.Link(self.inline(node)), address)
		return label + style.Address(" ("+address+")")

	case *ast.Image:
		address := style.Address("(" + string(node.Destination) + ")")

		words := self.inline(node)
		if style.Plain(words) == "" {
			return address
		}

		return style.Link(words) + " " + address

	case *ast.AutoLink:
		address := string(node.URL(self.source))
		if node.AutoLinkType == ast.AutoLinkEmail {
			address = "mailto:" + address
		}
		return self.hyperlink(style.Link(string(node.URL(self.source))), address)

	case *ast.RawHTML:
		return style.Subtle(self.raw(node))

	case *extensionast.TaskCheckBox:
		if node.IsChecked {
			return style.Success("[x] ")
		}

		return style.Subtle("[ ] ")
	}

	return self.inline(node)
}

func (self *renderer) hyperlink(text string, address string) string {
	if !self.shouldRenderHyperlinks {
		return text
	}

	return link.RenderWebURL(text, address)
}

func (self *renderer) text(node *ast.Text) string {
	value := node.Segment.Value(self.source)

	if node.IsRaw() {
		return string(value)
	}

	return string(util.ResolveEntityNames(util.ResolveNumericReferences(
		util.UnescapePunctuations(value),
	)))
}

func lineBreak(node *ast.Text) string {
	if node.SoftLineBreak() || node.HardLineBreak() {
		return "\n"
	}

	return ""
}

func (self *renderer) words(parent ast.Node) string {
	var out strings.Builder

	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		if textNode, is := node.(*ast.Text); is {
			out.Write(textNode.Segment.Value(self.source))
		}
	}

	return out.String()
}

func (self *renderer) raw(node *ast.RawHTML) string {
	var out strings.Builder

	for i := range node.Segments.Len() {
		segment := node.Segments.At(i)
		out.Write(segment.Value(self.source))
	}

	return out.String()
}

func over(paint style.Style, text string) string {
	stylePrefix := strings.TrimSuffix(paint(""), reset)
	if stylePrefix == "" {
		return text
	}

	return paint(strings.TrimSuffix(strings.ReplaceAll(text, reset, reset+stylePrefix), stylePrefix))
}
