package column

import (
	"strings"

	"crdx.org/oh/internal/app/width"
)

const gap = 2

func Rows(values []string, cells int) []string {
	if len(values) == 0 {
		return nil
	}

	columnWidth := 0
	for _, value := range values {
		columnWidth = max(columnWidth, width.Of(value)+gap)
	}

	var rows []string
	var row strings.Builder
	usedWidth := 0
	for _, value := range values {
		if usedWidth > 0 && usedWidth+columnWidth > cells {
			rows = append(rows, strings.TrimRight(row.String(), " "))
			row.Reset()
			usedWidth = 0
		}
		row.WriteString(value)
		row.WriteString(strings.Repeat(" ", columnWidth-width.Of(value)))
		usedWidth += columnWidth
	}

	return append(rows, strings.TrimRight(row.String(), " "))
}
