package html2md

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"crdx.org/oh/internal/textwriter"
)

func Convert(root *html.Node) string {
	var output textwriter.Writer
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		renderMarkdownNode(&output, child, false)
	}
	return strings.TrimSpace(output.String())
}

func renderMarkdownNode(output *textwriter.Writer, node *html.Node, isInline bool) {
	if node.Type == html.TextNode {
		output.Text(node.Data)
		return
	}
	if node.Type != html.ElementNode && node.Type != html.DocumentNode {
		return
	}

	switch node.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(node.Data[1] - '0')
		output.Newlines(2)
		output.Raw(strings.Repeat("#", level) + " ")
		renderMarkdownChildren(output, node, true)
		output.Newlines(2)
	case "p":
		if !isInline {
			output.Newlines(2)
		}
		renderMarkdownChildren(output, node, true)
		if !isInline {
			output.Newlines(2)
		}
	case "div", "section", "article", "header", "footer", "main", "figure", "figcaption", "details", "summary":
		if !isInline {
			output.Newlines(1)
		}
		renderMarkdownChildren(output, node, isInline)
		if !isInline {
			output.Newlines(1)
		}
	case "br":
		output.Newlines(1)
	case "hr":
		output.Newlines(2)
		output.Raw("---")
		output.Newlines(2)
	case "strong", "b":
		output.Raw("**")
		renderMarkdownChildren(output, node, true)
		output.Raw("**")
	case "em", "i":
		output.Raw("*")
		renderMarkdownChildren(output, node, true)
		output.Raw("*")
	case "code":
		output.Raw("`")
		renderMarkdownChildren(output, node, true)
		output.Raw("`")
	case "pre":
		output.Newlines(2)
		output.Raw("```\n" + strings.TrimSpace(nodeText(node)) + "\n```")
		output.Newlines(2)
	case "a":
		renderLink(output, node)
	case "img":
		renderImage(output, node)
	case "blockquote":
		renderBlockquote(output, node)
	case "ul":
		renderList(output, node, false, 0)
	case "ol":
		renderList(output, node, true, 0)
	case "li":
		renderMarkdownChildren(output, node, true)
	default:
		renderMarkdownChildren(output, node, isInline)
	}
}

func renderMarkdownChildren(output *textwriter.Writer, parent *html.Node, isInline bool) {
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		renderMarkdownNode(output, child, isInline)
	}
}

func renderLink(output *textwriter.Writer, node *html.Node) {
	href := attribute(node, "href")
	if href == "" {
		renderMarkdownChildren(output, node, true)
		return
	}
	output.Raw("[")
	renderMarkdownChildren(output, node, true)
	output.Raw("](" + href + ")")
}

func renderImage(output *textwriter.Writer, node *html.Node) {
	source := attribute(node, "src")
	if source == "" {
		return
	}
	output.Raw("![" + attribute(node, "alt") + "](" + source + ")")
}

func renderBlockquote(output *textwriter.Writer, node *html.Node) {
	var quotedText textwriter.Writer
	renderMarkdownChildren(&quotedText, node, false)
	text := strings.TrimSpace(quotedText.String())
	if text == "" {
		return
	}

	output.Newlines(2)
	for line := range strings.SplitSeq(text, "\n") {
		output.Raw("> " + line)
		output.Newlines(1)
	}
	output.Newlines(1)
}

func renderList(output *textwriter.Writer, node *html.Node, isOrdered bool, depth int) {
	output.Newlines(1)
	index := 1
	for item := node.FirstChild; item != nil; item = item.NextSibling {
		if item.Type != html.ElementNode || item.Data != "li" {
			continue
		}

		output.Raw(strings.Repeat("    ", depth))
		if isOrdered {
			output.Raw(fmt.Sprintf("%d. ", index))
			index++
		} else {
			output.Raw("- ")
		}

		for child := item.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && (child.Data == "ul" || child.Data == "ol") {
				renderList(output, child, child.Data == "ol", depth+1)
				continue
			}
			renderMarkdownNode(output, child, true)
		}
		output.Newlines(1)
	}
	output.Newlines(1)
}

func nodeText(node *html.Node) string {
	var output strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			output.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return output.String()
}

func attribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
