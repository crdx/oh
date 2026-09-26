package painter

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
)

func TestAThoughtLosesOnlyTheMarkdownThatWouldHaveBeenDrawn(t *testing.T) {
	for _, testCase := range []struct {
		thought string
		want    string
	}{
		{"**Checking** the file", "Checking the file"},
		{"*one* and _two_ and ~~three~~", "one and two and three"},
		{"***both*** at once", "both at once"},
		{"__strong__ words", "strong words"},
		{"foo_bar_baz", "foo_bar_baz"},
		{"calls read_file and write_file", "calls read_file and write_file"},
		{"_leading_underscore", "_leading_underscore"},
		{"trailing_underscore_", "trailing_underscore_"},
		{"_private field", "_private field"},
		{"__init__ method", "init method"},
		{"`foo_bar_baz`", "foo_bar_baz"},
		{"`**not bold**` stays", "**not bold** stays"},
		{"``has ` inside``", "has ` inside"},
		{"an unclosed ` tick", "an unclosed ` tick"},
		{"2 * 3 * 4", "2 * 3 * 4"},
		{"a ** b", "a ** b"},
		{"~/proj and ~/tmp", "~/proj and ~/tmp"},
		{"roughly ~5 to ~10", "roughly ~5 to ~10"},
		{"items[0] and map[key]", "items[0] and map[key]"},
		{"see [the docs](https://example.com) now", "see the docs (https://example.com) now"},
		{"![a picture](one.png)", "a picture (one.png)"},
		{"[**bold** label](x)", "bold label (x)"},
		{"an escaped \\*star\\*", "an escaped *star*"},
		{"snake_case in *italic*", "snake_case in italic"},
		{"*foo_bar*", "foo_bar"},
		{"x*y*z", "xyz"},
		{"_private and trailing_", "private and trailing"},
		{"~one~ and ~~two~~", "one and two"},
		{"**a*", "*a"},
		{"<https://x.y> and <b>html</b>", "https://x.y and <b>html</b>"},
		{"<me@localhost>", "me@localhost"},
		{"<not an autolink>", "<not an autolink>"},
		{"&amp; &lt; &copy; &#65; &#x41; &foo;", "& < © A A &foo;"},
		{"costs \\$5 and 50\\% or \\\"q\\\"", "costs $5 and 50% or \"q\""},
		{"hard\\\nbreak", "hard break"},
		{"a\\\\\nb", "a\\ b"},
		{"trail\\", "trail\\"},
		{"## Title ##", "Title"},
		{"# C#", "C#"},
		{"#hashtag", "#hashtag"},
		{"1. first\n2) second\n0. zero", "1. first 2. second 0. zero"},
		{"- [ ] todo", "• [ ] todo"},
		{"[a](https://en.wikipedia.org/wiki/Foo_(bar)) end", "a (https://en.wikipedia.org/wiki/Foo_(bar)) end"},
		{"[a](<has space> \"title\") end", "a (has space) end"},
		{"[a](url 'single') and [b](url (paren))", "a (url) and b (url)"},
		{"[a](x\\_y&amp;z)", "a (x\\_y&amp;z)"},
		{"[a] (url) and [a][b]", "[a] (url) and [a][b]"},
		{"[a]() end", "a () end"},
		{"![](pic.png) end", "(pic.png) end"},
		{"![**bold** alt](p.png)", "bold alt (p.png)"},
		{"[[nested]](u) and [a\\]b](u)", "[nested] (u) and a]b (u)"},
		{"`code [x](y)` and [`code`](u)", "code [x](y) and code (u)"},
		{"*[a](u)* and [a](u)_b_", "a (u) and a (u)b"},
		{"| a \\| b | `c` |", "a | b c"},
		{"````\n```\ninner_x *y*\n```\n````", "``` inner_x *y* ```"},
		{"~~~\ncode_x *y*\n~~~", "code_x *y*"},
		{"```\none\n~~~\ntwo_x\n```", "one ~~~ two_x"},
	} {
		rows := RenderReasoning(testCase.thought, 80, output.ReasoningPlain)
		for i := range rows {
			rows[i] = style.Plain(rows[i])
		}

		if got := strings.Join(rows, "\n"); got != testCase.want {
			t.Errorf("thought %q drew %q, want %q", testCase.thought, got, testCase.want)
		}
	}
}

func TestAnArrivingThoughtHoldsBackMarkdownThatMayYetClose(t *testing.T) {
	for _, testCase := range []struct {
		thought string
		want    string
	}{
		{"The **firs", "The firs"},
		{"The **", "The "},
		{"was read `one.g", "was read one.g"},
		{"was read `one_", "was read one_"},
		{"a *closed* one and foo_bar", "a closed one and foo_bar"},
		{"2 * 3", "2 * 3"},
		{"items[0", "items[0"},
		{"see [the docs", "see [the docs"},
		{"see [the docs]", "see [the docs]"},
		{"see [the docs](https://exa", "see the docs (https://exa)"},
		{"pic ![alt](p.pn", "pic alt (p.pn)"},
		{"auto <https://x.y", "auto <https://x.y"},
		{"costs \\", "costs "},
		{"a hard\\", "a hard"},
		{"## Title #", "Title"},
		{"_private", "private"},
		{"~/proj", "/proj"},
	} {
		var plain plainThought
		settled, tail := plain.Text(testCase.thought, false)

		if settled != "" {
			t.Errorf("thought %q settled %q before its line ended", testCase.thought, settled)
		}
		if tail != testCase.want {
			t.Errorf("arriving thought %q drew %q, want %q", testCase.thought, tail, testCase.want)
		}
	}
}
