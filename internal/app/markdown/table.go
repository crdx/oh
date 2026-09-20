package markdown

import (
	"slices"
	"strings"

	"crdx.org/col"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const widest = 30

func (self *renderer) table(node *extensionast.Table) []string {
	var head []string
	var body [][]string

	for row := node.FirstChild(); row != nil; row = row.NextSibling() {
		cells := self.cellsOf(row)

		if _, is := row.(*extensionast.TableHeader); is {
			head = cells
			continue
		}

		body = append(body, cells)
	}

	if len(head) == 0 {
		return nil
	}

	columns := len(head)

	availableColumns := self.columns - (3*columns + 1)
	if availableColumns < columns {
		return self.plainTableRows(head, body)
	}

	widths := layout(head, body, availableColumns)

	rows := []string{border(widths, "┌", "┬", "┐")}
	rows = append(rows, tableRows(head, node.Alignments, widths, true)...)
	rows = append(rows, border(widths, "├", "┼", "┤"))

	for i, one := range body {
		if i > 0 {
			rows = append(rows, border(widths, "├", "┼", "┤"))
		}

		rows = append(rows, tableRows(one, node.Alignments, widths, false)...)
	}

	return append(rows, border(widths, "└", "┴", "┘"))
}

func (self *renderer) cellsOf(row ast.Node) []string {
	var cells []string

	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		cells = append(cells, self.linkPaths(self.inline(cell)))
	}

	return cells
}

func (self *renderer) plainTableRows(head []string, body [][]string) []string {
	var rows []string

	for _, one := range append([][]string{head}, body...) {
		rows = append(rows, width.Wrap(strings.Join(one, style.Border(" | ")), self.columns)...)
	}

	return rows
}

func layout(head []string, body [][]string, availableColumns int) []int {
	columns := len(head)

	natural := make([]int, columns)
	minimum := make([]int, columns)

	for at := range columns {
		natural[at] = style.Width(head[at])
		minimum[at] = longest(head[at])

		for _, row := range body {
			if at >= len(row) {
				continue
			}

			natural[at] = max(natural[at], style.Width(row[at]))
			minimum[at] = max(minimum[at], longest(row[at]))
		}

		minimum[at] = min(max(minimum[at], 1), widest)
		natural[at] = max(natural[at], minimum[at])
	}

	if total(natural) <= availableColumns {
		return natural
	}

	if total(minimum) > availableColumns {
		return share(slices.Repeat([]int{1}, columns), minimum, availableColumns)
	}

	return share(minimum, natural, availableColumns)
}

func share(base []int, want []int, availableColumns int) []int {
	widths := slices.Clone(base)

	growth := total(want) - total(base)
	spare := availableColumns - total(base)

	if growth > 0 && spare > 0 {
		for at := range widths {
			widths[at] += (want[at] - base[at]) * spare / growth
		}
	}

	for at := range availableColumns - total(widths) {
		widths[at%len(widths)]++
	}

	return widths
}

func border(widths []int, left string, between string, right string) string {
	parts := make([]string, len(widths))

	for at, cells := range widths {
		parts[at] = strings.Repeat("─", cells+2)
	}

	return style.Border(left + strings.Join(parts, between) + right)
}

func tableRows(cells []string, aligns []extensionast.Alignment, widths []int, isHeading bool) []string {
	wrappedCells := make([][]string, len(widths))
	height := 1

	for at := range widths {
		styledCell := ""
		if at < len(cells) {
			styledCell = cells[at]
		}

		if isHeading {
			styledCell = over(col.Bold, styledCell)
		}

		wrappedCells[at] = width.Wrap(styledCell, widths[at])
		height = max(height, len(wrappedCells[at]))
	}

	bar := style.Border("│")
	rows := make([]string, height)

	for line := range height {
		var out strings.Builder

		out.WriteString(bar)

		for at := range widths {
			text := ""
			if line < len(wrappedCells[at]) {
				text = wrappedCells[at][line]
			}

			out.WriteString(" ")
			out.WriteString(pad(text, widths[at], alignment(aligns, at)))
			out.WriteString(" ")
			out.WriteString(bar)
		}

		rows[line] = out.String()
	}

	return rows
}

func alignment(aligns []extensionast.Alignment, at int) extensionast.Alignment {
	if at < len(aligns) {
		return aligns[at]
	}

	return extensionast.AlignNone
}

func pad(text string, cells int, align extensionast.Alignment) string {
	spare := cells - style.Width(text)
	if spare <= 0 {
		return text
	}

	switch align {
	case extensionast.AlignRight:
		return strings.Repeat(" ", spare) + text
	case extensionast.AlignCenter:
		return strings.Repeat(" ", spare/2) + text + strings.Repeat(" ", spare-spare/2)
	case extensionast.AlignLeft, extensionast.AlignNone:
	}

	return text + strings.Repeat(" ", spare)
}

func longest(cell string) int {
	most := 0

	for word := range strings.FieldsSeq(style.Plain(cell)) {
		most = max(most, width.Of(word))
	}

	return most
}

func total(values []int) int {
	sum := 0

	for _, one := range values {
		sum += one
	}

	return sum
}
