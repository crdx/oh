package markdown

import (
	"regexp"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/style"
)

var urlPattern = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.-]*://[^\\s'\"`<>|;()]+")

func HighlightURLs(command string) string {
	spans, err := bashCommandSpans(command)
	if err != nil {
		spans = nil
	}

	return paintSpans(command, withURLs(spans, urlSpans(command)), false)
}

func urlSpans(command string) []sourceSpan {
	var spans []sourceSpan

	for _, place := range urlPattern.FindAllStringIndex(command, -1) {
		end := place[1]
		for end > place[0] && strings.ContainsRune(".,;:", rune(command[end-1])) {
			end--
		}

		spans = append(spans, sourceSpan{start: place[0], end: end, style: style.Hazard})
	}

	return spans
}

func withURLs(spans []sourceSpan, urls []sourceSpan) []sourceSpan {
	if len(urls) == 0 {
		return spans
	}

	overlaid := make([]sourceSpan, 0, len(spans)+len(urls))
	for _, span := range spans {
		overlaid = append(overlaid, aroundURLs(span, urls)...)
	}

	overlaid = append(overlaid, urls...)
	slices.SortFunc(overlaid, bySourcePosition)

	return overlaid
}

func aroundURLs(span sourceSpan, urls []sourceSpan) []sourceSpan {
	var pieces []sourceSpan
	start := span.start

	for _, url := range urls {
		if url.end <= start {
			continue
		}
		if url.start >= span.end {
			break
		}

		if start < url.start {
			pieces = append(pieces, sourceSpan{start: start, end: url.start, style: span.style})
		}

		start = max(start, url.end)
	}

	if start < span.end {
		pieces = append(pieces, sourceSpan{start: start, end: span.end, style: span.style})
	}

	return pieces
}
