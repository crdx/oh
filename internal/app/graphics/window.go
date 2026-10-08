package graphics

import "strings"

func HiddenPictureTransmissions(hiddenRows []string, visibleRows []string) string {
	var transmissions strings.Builder
	var visibleText string

	for _, row := range hiddenRows {
		if !strings.HasPrefix(row, openCommand+"a=T,") {
			continue
		}

		placeholderAt := strings.Index(row, placeholder)
		if placeholderAt < 0 {
			continue
		}

		colourAt := strings.LastIndex(row[:placeholderAt], "\x1b[38;2;")
		if colourAt < 0 {
			continue
		}

		colourEnd := strings.IndexByte(row[colourAt:placeholderAt], 'm')
		if colourEnd < 0 {
			continue
		}

		colour := row[colourAt : colourAt+colourEnd+1]
		if visibleText == "" {
			visibleText = strings.Join(visibleRows, "")
		}
		if !strings.Contains(visibleText, colour+placeholder) {
			continue
		}

		transmissions.WriteString(row[:colourAt])
	}

	return transmissions.String()
}
