package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testHTML = `<!DOCTYPE html>
<html>
<head><style>hidden { color: red }</style></head>
<body>
<nav>Menu</nav>
<h1>Example &amp; test</h1>
<p>Read <strong>this</strong> <a href="https://example.com/more">page</a>.</p>
<ul><li>First</li><li>Second</li></ul>
<script>doBadThings()</script>
</body>
</html>`

func allowFetch(context.Context, string) error { return nil }

func saveTestHTML([]byte) (string, error) {
	return "/session/drops/fetch-test.html", nil
}

func TestFetchReturnsEverySupportedFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "text/html, application/xhtml+xml" {
			t.Errorf("got accept header %q", request.Header.Get("Accept"))
		}
		_, _ = writer.Write([]byte(testHTML))
	}))
	defer server.Close()

	page, err := fetchPage(t.Context(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}

	for format, want := range map[string][]string{
		"raw":        {"<!DOCTYPE html>", "doBadThings()"},
		"clean_html": {"<h1>Example &amp; test</h1>", "<nav>Menu</nav>"},
		"text":       {"Example & test", "Read this page.", "First", "Second"},
		"markdown":   {"# Example & test", "**this**", "[page](https://example.com/more)", "- First"},
	} {
		got, err := renderPage(page.rawHTML, format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		for _, fragment := range want {
			if !strings.Contains(got, fragment) {
				t.Errorf("%s: expected %q in %q", format, fragment, got)
			}
		}
		if format != "raw" && strings.Contains(got, "doBadThings") {
			t.Errorf("%s retained stripped script: %q", format, got)
		}
	}
}

func TestFetchSavesRawHTMLAndReturnsItsPathForEverySupportedFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(testHTML))
	}))
	defer server.Close()

	for _, format := range []string{"raw", "clean_html", "text", "markdown"} {
		var savedHTML []byte
		call, err := newTool(
			func() bool { return true },
			allowFetch,
			func(contents []byte) (string, error) {
				savedHTML = append(savedHTML[:0], contents...)
				return "/session/drops/fetch-test.html", nil
			},
			server.Client(),
		).Parse(`{"url":"` + server.URL + `","type":"` + format + `"}`)
		if err != nil {
			t.Fatal(err)
		}

		result, err := call.Exec(t.Context())
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if string(savedHTML) != testHTML {
			t.Errorf("%s saved %q, want the unaltered HTML", format, savedHTML)
		}
		if !strings.HasPrefix(result.Output, "[raw HTML saved to /session/drops/fetch-test.html]\n\n") {
			t.Errorf("%s returned %q", format, result.Output)
		}
	}
}

func TestFetchParsesMalformedHTMLAsADocumentTree(t *testing.T) {
	page := `<body><p data-note="1 > 0">one <b>two<p>three<script>bad()`
	cleanHTML := fetchTestPage(t, page, "clean_html")

	for _, want := range []string{`data-note="1 &gt; 0"`, "<b>two</b>", "<p><b>three</b></p>"} {
		if !strings.Contains(cleanHTML, want) {
			t.Errorf("expected %q in %q", want, cleanHTML)
		}
	}
	if strings.Contains(cleanHTML, "bad()") {
		t.Errorf("script survived in %q", cleanHTML)
	}

	text := fetchTestPage(t, page, "text")
	if text != "one two\nthree" {
		t.Errorf("got text %q", text)
	}
}

func fetchTestPage(t *testing.T, contents string, format string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(contents))
	}))
	defer server.Close()

	page, err := fetchPage(t.Context(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	output, err := renderPage(page.rawHTML, format)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestFetchRejectsInvalidURLsAndFormats(t *testing.T) {
	for _, args := range []Args{
		{URL: "relative", Type: "text"},
		{URL: "https://example.com", Type: "pdf"},
	} {
		if err := validate(args); err == nil {
			t.Errorf("expected %#v to be rejected", args)
		}
	}
}

func TestFetchReportsHTTPFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
		_, _ = writer.Write([]byte("not today"))
	}))
	defer server.Close()

	var savedHTML []byte
	call, err := newTool(
		func() bool { return true },
		allowFetch,
		func(contents []byte) (string, error) {
			savedHTML = append(savedHTML, contents...)
			return "/session/drops/fetch-error.html", nil
		},
		server.Client(),
	).Parse(`{"url":"` + server.URL + `","type":"text"}`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = call.Exec(t.Context())
	if err == nil || !strings.Contains(err.Error(), "HTTP 418: not today") ||
		!strings.Contains(err.Error(), "/session/drops/fetch-error.html") {
		t.Errorf("got %v", err)
	}
	if string(savedHTML) != "not today" {
		t.Errorf("saved %q, want the HTTP failure body", savedHTML)
	}
}

func TestFetchReportsWhenRawHTMLCannotBeSaved(t *testing.T) {
	saveFailure := errors.New("drops are unavailable")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(testHTML))
	}))
	defer server.Close()

	call, err := newTool(
		func() bool { return true },
		allowFetch,
		func([]byte) (string, error) { return "", saveFailure },
		server.Client(),
	).Parse(`{"url":"` + server.URL + `","type":"markdown"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, saveFailure) {
		t.Errorf("got %v, want the save failure", err)
	}
}

func TestFetchIsAReadOnlyConcurrentTool(t *testing.T) {
	offeredTool := New(func() bool { return true }, allowFetch, saveTestHTML)

	if offeredTool.Name() != "fetch" {
		t.Errorf("got name %q", offeredTool.Name())
	}
	if !offeredTool.Concurrent() {
		t.Error("expected the tool to be concurrent")
	}
	if !offeredTool.ReadOnly() {
		t.Error("expected the tool to be read-only")
	}
}

func TestFetchIsRefusedWithoutNetworkAccess(t *testing.T) {
	var wasReached bool

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		wasReached = true
		_, _ = writer.Write([]byte(testHTML))
	}))
	defer server.Close()

	call, err := newTool(func() bool { return false }, allowFetch, saveTestHTML, server.Client()).
		Parse(`{"url":"` + server.URL + `","type":"text"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, ErrWithheld) {
		t.Errorf("got %v, want %v", err, ErrWithheld)
	}
	if wasReached {
		t.Error("the page was fetched anyway")
	}
}

func TestFetchReportsAPageWithNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	call, err := newTool(func() bool { return true }, allowFetch, saveTestHTML, server.Client()).
		Parse(`{"url":"` + server.URL + `","type":"text"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); err == nil || !strings.Contains(err.Error(), "no content") ||
		!strings.Contains(err.Error(), "/session/drops/fetch-test.html") {
		t.Errorf("got %v", err)
	}
}

func TestARefusedFetchReachesNothing(t *testing.T) {
	wasReached := false
	refusal := errors.New("the user refused this fetch")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		wasReached = true
		_, _ = writer.Write([]byte(testHTML))
	}))
	defer server.Close()

	asked := ""
	call, err := newTool(
		func() bool { return true },
		func(_ context.Context, address string) error {
			asked = address
			return refusal
		},
		saveTestHTML,
		server.Client(),
	).Parse(`{"url":"` + server.URL + `","type":"text"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, refusal) {
		t.Errorf("got %v, want the refusal", err)
	}
	if wasReached {
		t.Error("the page was fetched anyway")
	}
	if asked != server.URL {
		t.Errorf("got approval for %q, want %q", asked, server.URL)
	}
}
