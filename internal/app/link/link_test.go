package link

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/sandbox"
)

func TestURLLinkKeepsTheCompleteAddressVisible(t *testing.T) {
	address := "https://example.test/authorise?token=one"
	got := RenderURL(address, address)
	want := "\x1b]8;;" + address + "\x1b\\" + address + "\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if Plain(got) != address {
		t.Errorf("visible address is %q", Plain(got))
	}
}

func TestWebURLsAreLinkedOnlyForSupportedSchemes(t *testing.T) {
	for name, test := range map[string]struct {
		address      string
		shouldRender bool
	}{
		"http":             {address: "http://example.test/path", shouldRender: true},
		"https":            {address: "https://example.test/path", shouldRender: true},
		"email":            {address: "mailto:person@example.test", shouldRender: true},
		"encoded space":    {address: "https://example.test/a b", shouldRender: true},
		"relative":         {address: "docs/page.html"},
		"missing host":     {address: "https:path"},
		"unsupported":      {address: "javascript:alert(1)"},
		"terminal control": {address: "https://example.test/\x1b]8;;bad"},
	} {
		got := RenderWebURL("label", test.address)
		wasRendered := got != "label"
		if wasRendered != test.shouldRender {
			t.Errorf("%s: rendered=%t: %q", name, wasRendered, got)
		}
		if Plain(got) != "label" {
			t.Errorf("%s: visible label = %q", name, Plain(got))
		}
	}
}

func TestSourceLocationsBecomeFileFragmentsWithoutChangingTheirText(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "cmd/oh/draw.go")

	for _, test := range []struct {
		location string
		fragment string
	}{
		{location: "cmd/oh/draw.go:42", fragment: "42"},
		{location: "cmd/oh/draw.go:42:7", fragment: "42:7"},
	} {
		got := Render("see "+test.location+" and carry on", Roots{Workspace: workspace})
		address := linkAddress(t, got)

		if address.Scheme != "file" || address.Path != filepath.ToSlash(path) || address.Fragment != test.fragment {
			t.Errorf("got address %q", address)
		}
		if stripEscapes(got) != "see "+test.location+" and carry on" {
			t.Errorf("expected the visible text unchanged, got %q", stripEscapes(got))
		}
	}
}

func TestAPathCanLinkItsLabelAtALine(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "cmd/oh/draw.go")
	text := "cmd/oh/draw.go 42-47"

	got := RenderPathAtLine(text, "cmd/oh/draw.go", Roots{Workspace: workspace}, "42")
	address := linkAddress(t, got)

	if address.Scheme != "file" || address.Path != filepath.ToSlash(path) || address.Fragment != "42" {
		t.Errorf("got address %q", address)
	}
	if stripEscapes(got) != text {
		t.Errorf("expected the visible text unchanged, got %q", stripEscapes(got))
	}
	if beforeClose, _, _ := strings.Cut(got, closeLink); !strings.Contains(beforeClose, "42-47") {
		t.Errorf("expected the range inside the link, got %q", got)
	}
}

func TestPathsWithSpacesBecomeLinks(t *testing.T) {
	workspace := t.TempDir()
	relativePath := prepareFile(t, workspace, "reports/final draft.txt")
	absolutePath := prepareFile(t, t.TempDir(), "screenshots/first frame.png")

	text := "read final draft.txt and " + absolutePath + ":12:3."
	got := Render(text, Roots{Workspace: filepath.Dir(relativePath)})

	addresses := linkAddresses(t, got)
	if len(addresses) != 2 {
		t.Fatalf("got %d links in %q, want two", len(addresses), got)
	}
	if addresses[0].Path != filepath.ToSlash(relativePath) || addresses[0].Fragment != "" {
		t.Errorf("first address is %q", addresses[0])
	}
	if addresses[1].Path != filepath.ToSlash(absolutePath) || addresses[1].Fragment != "12:3" {
		t.Errorf("second address is %q", addresses[1])
	}
	if Plain(got) != text {
		t.Errorf("visible text is %q, want %q", Plain(got), text)
	}
}

func TestAPathAlreadyLinkedToTheWebIsNotNestedInAFileLink(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "cmd/oh/draw.go")
	linkedPath := RenderWebURL("cmd/oh/draw.go", "https://example.test/source")

	if got := Render("read "+linkedPath, Roots{Workspace: workspace}); got != "read "+linkedPath {
		t.Errorf("already-linked path changed: %q", got)
	}
}

func TestAPathSplitAcrossStylesBecomesOneLink(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "cmd/oh/draw.go")
	styledPath := "\x1b[2mcmd/oh/\x1b[0m\x1b[1mdraw.go\x1b[0m"

	got := Render("read "+styledPath, Roots{Workspace: workspace})

	if strings.Count(got, openPrefix) != 2 {
		t.Errorf("expected one opening and one closing sequence, got %q", got)
	}
	if withoutLinks := stripHyperlinks(got); withoutLinks != "read "+styledPath {
		t.Errorf("expected the styles within the path unchanged, got %q", withoutLinks)
	}
	if stripEscapes(got) != "read cmd/oh/draw.go" {
		t.Errorf("expected the visible text unchanged, got %q", stripEscapes(got))
	}
}

func TestAbsoluteDirectoriesAndSpecialURLCharacters(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "100%.txt")

	got := Render(workspace+" "+filepath.Base(path), Roots{Workspace: workspace})

	if strings.Count(got, openPrefix) != 4 {
		t.Errorf("expected two links, got %q", got)
	}
	if !strings.Contains(got, "100%25.txt") {
		t.Errorf("expected an escaped URL, got %q", got)
	}
}

func TestMultiDotAndHiddenFilenamesBecomeLinks(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "AGENTS.local.md")
	prepareFile(t, workspace, ".gitignore")

	got := Render("AGENTS.local.md and .gitignore", Roots{Workspace: workspace})

	if strings.Count(got, openPrefix) != 4 {
		t.Errorf("expected two links, got %q", got)
	}
	if stripEscapes(got) != "AGENTS.local.md and .gitignore" {
		t.Errorf("expected filenames unchanged, got %q", stripEscapes(got))
	}
}

func TestAPathAfterAnAssignmentIsLinkedWithoutItsName(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "changes.patch")

	for _, test := range []struct {
		name string
		text string
		lead string
	}{
		{name: "variable", text: "PATCH=" + path, lead: "PATCH="},
		{name: "flag", text: "--output=" + path, lead: "--output="},
		{name: "relative", text: "PATCH=changes.patch", lead: "PATCH="},
	} {
		got := Render(test.text, Roots{Workspace: workspace})

		if address := linkAddress(t, got); address.Path != filepath.ToSlash(path) {
			t.Errorf("%s: got address %q", test.name, address)
		}
		if stripEscapes(got) != test.text {
			t.Errorf("%s: expected the visible text unchanged, got %q", test.name, stripEscapes(got))
		}
		if before, _, _ := strings.Cut(got, openPrefix); before != test.lead {
			t.Errorf("%s: expected the name outside the link, got %q", test.name, got)
		}
	}
}

func TestAFilenameHoldingAnEqualsSignIsLinkedWhole(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "a=b/c.txt")

	got := Render("read a=b/c.txt", Roots{Workspace: workspace})

	if stripHyperlinks(got) != "read a=b/c.txt" {
		t.Errorf("expected the visible text unchanged, got %q", stripHyperlinks(got))
	}
	if before, _, _ := strings.Cut(got, openPrefix); before != "read " {
		t.Errorf("expected the whole name linked, got %q", got)
	}
}

func TestAPathClosingASentenceIsLinkedWithoutItsFullStop(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "changes.patch")

	got := Render("Patch is ready at "+path+".", Roots{Workspace: workspace})

	if address := linkAddress(t, got); address.Path != filepath.ToSlash(path) {
		t.Errorf("got address %q", address)
	}
	if stripEscapes(got) != "Patch is ready at "+path+"." {
		t.Errorf("expected the visible text unchanged, got %q", stripEscapes(got))
	}
	if _, after, _ := strings.Cut(got, closeLink); after != "." {
		t.Errorf("expected the full stop outside the link, got %q", got)
	}
}

func TestMissingPathsAndOrdinaryDottedWordsStayPlain(t *testing.T) {
	text := "missing.go and example.com are not files here"

	if got := Render(text, Roots{Workspace: t.TempDir()}); got != text {
		t.Errorf("got %q, want unchanged text", got)
	}
}

func linkAddress(t *testing.T, rendered string) *url.URL {
	t.Helper()

	addresses := linkAddresses(t, rendered)
	if len(addresses) == 0 {
		t.Fatalf("no hyperlink in %q", rendered)
	}

	return addresses[0]
}

func linkAddresses(t *testing.T, rendered string) []*url.URL {
	t.Helper()

	var addresses []*url.URL
	for {
		begin := strings.Index(rendered, openPrefix)
		if begin < 0 {
			return addresses
		}
		rendered = rendered[begin+len(openPrefix):]
		end := strings.Index(rendered, terminator)
		if end < 0 {
			t.Fatalf("unterminated hyperlink in %q", rendered)
		}
		if end > 0 {
			address, err := url.Parse(rendered[:end])
			if err != nil {
				t.Fatalf("parse hyperlink: %v", err)
			}
			addresses = append(addresses, address)
		}
		rendered = rendered[end+len(terminator):]
	}
}

func prepareFile(t *testing.T, workspace string, name string) string {
	t.Helper()

	path := filepath.Join(workspace, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("prepare directory: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}
	return path
}

func stripHyperlinks(text string) string {
	text = strings.ReplaceAll(text, closeLink, "")
	for {
		begin := strings.Index(text, openPrefix)
		if begin < 0 {
			return text
		}
		end := strings.Index(text[begin:], terminator)
		if end < 0 {
			return text
		}
		text = text[:begin] + text[begin+end+len(terminator):]
	}
}

func stripEscapes(text string) string {
	var plain strings.Builder

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			i = escapeEnd(text, i)
			continue
		}
		plain.WriteByte(text[i])
		i++
	}

	return plain.String()
}

func TestTheScratchAliasLinksToTheHostScratchAlias(t *testing.T) {
	scratch := t.TempDir()
	path := prepareFile(t, scratch, "io/cmd/oh/output/region.go")
	text := ScratchAlias + "/io/cmd/oh/output/region.go"

	got := Render("read "+text, Roots{Scratch: scratch})

	address := linkAddress(t, got)
	if address.Path != filepath.ToSlash(path) {
		t.Errorf("linked %q, want %q", address.Path, path)
	}
	if Plain(got) != "read "+text {
		t.Errorf("visible text is %q, want the compact scratch path", Plain(got))
	}
}

func TestAPathUnderTheModelsOwnTmpLinksToTheScratchItMapsTo(t *testing.T) {
	scratch := t.TempDir()
	path := prepareFile(t, scratch, "zoom-on.png")

	got := Render("see /tmp/zoom-on.png for it", Roots{Scratch: scratch})

	address := linkAddress(t, got)
	if address.Path != filepath.ToSlash(path) {
		t.Errorf("linked %q, want %q", address.Path, path)
	}
	if Plain(got) != "see /tmp/zoom-on.png for it" {
		t.Errorf("visible text is %q, want the path as the model wrote it", Plain(got))
	}
}

func TestAPathUnderTmpIsNeverSoughtOnTheHostWhereAScratchStandsForIt(t *testing.T) {
	scratch := t.TempDir()
	host := prepareFile(t, t.TempDir(), "only-here.png")

	text := "see " + filepath.Join(sandbox.TmpDir, filepath.Base(host)) + " for it"
	if got := Render(text, Roots{Scratch: scratch}); got != text {
		t.Errorf("got %q, want the host's own /tmp left alone", got)
	}
}

func TestAPathUnderTmpIsTakenAsTheHostsOwnWhereNoScratchStandsForIt(t *testing.T) {
	path := prepareFile(t, t.TempDir(), "unconfined.txt")

	got := Render("read "+path, Roots{})

	if !strings.Contains(got, openPrefix) || Plain(got) != "read "+path {
		t.Errorf("got %q, want the host's own path linked", got)
	}
}

func TestAPathOutsideTmpIsStillFoundUnderTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "notes.md")

	got := Render("read notes.md", Roots{Workspace: workspace, Scratch: t.TempDir()})

	if Plain(got) != "read notes.md" || !strings.Contains(got, openPrefix) {
		t.Errorf("got %q, want the workspace path linked", got)
	}
}
