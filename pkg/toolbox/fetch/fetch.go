package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"crdx.org/oh/internal/html2md"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
)

const (
	fetchTimeout      = 30 * time.Second
	maxFetchBytes     = 8 * 1024 * 1024
	rawHTMLPathNotice = "[raw HTML saved to %s]"
	userAgent         = "oh fetch"
)

var ErrWithheld = errors.New("network access unavailable; ctrl+x n grants it")

type Args struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type fetchedPage struct {
	rawHTML    []byte
	statusCode int
}

func defaultClient() *http.Client {
	return &http.Client{Timeout: fetchTimeout, Transport: publicTransport()}
}

func New(
	isAllowed func() bool,
	approve func(context.Context, string) error,
	saveRawHTML func([]byte) (string, error),
) tool.Tool {
	return newTool(isAllowed, approve, saveRawHTML, defaultClient())
}

func newTool(
	isAllowed func() bool,
	approve func(context.Context, string) error,
	saveRawHTML func([]byte) (string, error),
	client *http.Client,
) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "fetch",
			Description: "fetch a web page as markdown, clean HTML, text, or raw",
			Schema: tool.Schema{
				tool.String("url", "URL to fetch"),
				tool.String("type", "one of: markdown, clean_html, text, raw"),
			},
		},
		func(args Args) (string, string) { return args.URL, args.Type },
	).
		Validate(validate).
		Focuses(func(call tool.ToolCall) string { return call.Subject() }).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Requires(isAllowed, ErrWithheld).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			if err := approve(ctx, args.URL); err != nil {
				return "", tool.ToolCallMetrics{}, err
			}

			page, err := fetchPage(ctx, client, args.URL)
			if err != nil {
				return "", tool.ToolCallMetrics{}, err
			}
			rawHTMLPath, err := saveRawHTML(page.rawHTML)
			if err != nil {
				return "", tool.ToolCallMetrics{}, fmt.Errorf("save raw HTML: %w", err)
			}
			if page.statusCode < http.StatusOK || page.statusCode >= http.StatusMultipleChoices {
				return "", tool.ToolCallMetrics{}, fmt.Errorf(
					"fetch returned HTTP %d: %s (raw HTML saved to %s)",
					page.statusCode,
					strings.TrimSpace(string(page.rawHTML)),
					rawHTMLPath,
				)
			}

			output, err := renderPage(page.rawHTML, args.Type)
			if err != nil {
				return "", tool.ToolCallMetrics{}, fmt.Errorf(
					"render fetched page (raw HTML saved to %s): %w", rawHTMLPath, err,
				)
			}
			if output == "" {
				return "", tool.ToolCallMetrics{}, fmt.Errorf(
					"fetch returned no content (raw HTML saved to %s)", rawHTMLPath,
				)
			}
			output = fmt.Sprintf(rawHTMLPathNotice, rawHTMLPath) + "\n\n" + output

			return output, tool.GetMetrics(output), nil
		})
}

func validate(args Args) error {
	address, err := url.Parse(args.URL)
	if err != nil || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") {
		return errors.New("url must be an absolute HTTP(S) URL")
	}

	switch args.Type {
	case "markdown", "clean_html", "text", "raw":
		return nil
	default:
		return errors.New("type must be markdown, clean_html, text, or raw")
	}
}

func fetchPage(ctx context.Context, client *http.Client, address string) (fetchedPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fetchedPage{}, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/html, application/xhtml+xml")

	response, err := client.Do(request)
	if err != nil {
		return fetchedPage{}, fmt.Errorf("fetch failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxFetchBytes+1))
	if err != nil {
		return fetchedPage{}, fmt.Errorf("failed to read page: %w", err)
	}
	if len(contents) > maxFetchBytes {
		return fetchedPage{}, fmt.Errorf("page exceeds the %s limit", util.FormatBytes(maxFetchBytes, 3))
	}

	return fetchedPage{rawHTML: contents, statusCode: response.StatusCode}, nil
}

func renderPage(contents []byte, format string) (string, error) {
	if format == "raw" {
		return string(contents), nil
	}

	document, err := html.Parse(bytes.NewReader(contents))
	if err != nil {
		return "", fmt.Errorf("failed to parse page: %w", err)
	}
	removeUnwantedNodes(document)

	root := findElement(document, "body")
	if root == nil {
		root = document
	}

	switch format {
	case "clean_html":
		return renderChildren(root)
	case "text":
		return renderText(root), nil
	case "markdown":
		return html2md.Convert(root), nil
	default:
		panic("validated fetch type became unknown")
	}
}
