package fetch

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"crdx.org/io/internal/textwriter"
)

func removeUnwantedNodes(parent *html.Node) {
	for child := parent.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.CommentNode || child.Type == html.ElementNode && isUnwantedElement(child.Data) {
			parent.RemoveChild(child)
		} else {
			removeUnwantedNodes(child)
		}
		child = next
	}
}

func isUnwantedElement(name string) bool {
	switch name {
	case "script", "style", "noscript", "svg", "iframe":
		return true
	default:
		return false
	}
}

func findElement(node *html.Node, name string) *html.Node {
	if node.Type == html.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func renderChildren(parent *html.Node) (string, error) {
	var renderedText bytes.Buffer
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&renderedText, child); err != nil {
			return "", fmt.Errorf("failed to render page: %w", err)
		}
	}
	return strings.TrimSpace(renderedText.String()), nil
}

func renderText(root *html.Node) string {
	var output textwriter.Writer
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		renderTextNode(&output, child)
	}
	return strings.TrimSpace(output.String())
}

func renderTextNode(output *textwriter.Writer, node *html.Node) {
	if node.Type == html.TextNode {
		output.Text(node.Data)
		return
	}
	if node.Type != html.ElementNode && node.Type != html.DocumentNode {
		return
	}

	if node.Data == "br" {
		output.Newlines(1)
		return
	}
	isBlock := isBlockElement(node.Data)
	if isBlock {
		output.Newlines(1)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		renderTextNode(output, child)
	}
	if isBlock {
		output.Newlines(1)
	}
}

func isBlockElement(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "dd", "details", "div", "dl", "dt",
		"figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header",
		"hr", "li", "main", "nav", "ol", "p", "pre", "section", "summary", "table", "tbody",
		"td", "tfoot", "th", "thead", "tr", "ul":
		return true
	default:
		return false
	}
}
