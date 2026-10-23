package html2md

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestConvertRendersSemanticMarkdown(t *testing.T) {
	root := parseBody(t, `<body>
<h2>Heading</h2>
<p>Read <strong>this</strong> <a href="https://example.com">page</a>.</p>
<blockquote><p>Quoted <em>text</em>.</p></blockquote>
<ol><li>first<ul><li>nested</li></ul></li></ol>
<pre><code>if x &gt; 0 {
  go()
}</code></pre>
<img src="/diagram.png" alt="diagram">
</body>`)
	markdown := Convert(root)

	for _, want := range []string{
		"## Heading",
		"Read **this** [page](https://example.com).",
		"> Quoted *text*.",
		"1. first",
		"    - nested",
		"```\nif x > 0 {\n  go()\n}\n```",
		"![diagram](/diagram.png)",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("expected %q in %q", want, markdown)
		}
	}
}

func TestConvertHandlesFragmentsWithoutText(t *testing.T) {
	root := parseBody(t, `<body><hr><img></body>`)
	if got := Convert(root); got != "---" {
		t.Errorf("got %q", got)
	}
}

func TestAnOrderedListKeepsTheNumberItStartsAt(t *testing.T) {
	root := parseBody(t, `<body><ol start="5"><li>fifth</li><li>sixth</li></ol></body>`)
	if got, want := Convert(root), "5. fifth\n6. sixth"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestALineBreakInPreformattedTextStartsANewLine(t *testing.T) {
	root := parseBody(t, `<body><pre>first<br>second<br/>third</pre></body>`)
	if got, want := Convert(root), "```\nfirst\nsecond\nthird\n```"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func parseBody(t *testing.T, source string) *html.Node {
	t.Helper()

	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	for node := range document.Descendants() {
		if node.Type == html.ElementNode && node.Data == "body" {
			return node
		}
	}
	t.Fatal("parsed document has no body")
	return nil
}
