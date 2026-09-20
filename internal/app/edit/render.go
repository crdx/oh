package edit

import (
	"crdx.org/oh/internal/app/width"
)

const maxRows = 10

func window(rows []string, cursorRow int) Frame {
	visibleRows := width.WindowRows(rows, maxRows, cursorRow)

	return Frame{
		Rows:             visibleRows.Rows,
		Row:              visibleRows.Focus,
		HiddenLinesAbove: visibleRows.HiddenLinesAbove,
		HiddenLinesBelow: visibleRows.HiddenLinesBelow,
	}
}

func layout(buffer *Buffer, room int) ([]string, int, int) {
	runes := buffer.Runes()
	laid := width.Rows(string(runes), room)

	rows := make([]string, 0, len(laid)+1)
	for _, row := range laid {
		rows = append(rows, row.Text)
	}

	if buffer.Cursor() == len(runes) && hasTrailingCursorRow(laid, room) {
		return append(rows, ""), len(rows), 0
	}

	cursorRow, cursorColumn := locate(laid, runes, buffer.Cursor(), room)

	return rows, cursorRow, cursorColumn
}

func hasTrailingCursorRow(rows []width.Row, room int) bool {
	return room > 0 && width.Of(rows[len(rows)-1].Text) >= room
}

func moveCursorVertically(buffer *Buffer, room int, direction int) bool {
	runes := buffer.Runes()
	rows := width.Rows(string(runes), room)
	cursorRow, cursorColumn := locate(rows, runes, buffer.Cursor(), room)

	if hasTrailingCursorRow(rows, room) {
		rows = append(rows, width.Row{Begin: len(runes), End: len(runes), Next: len(runes)})
		if buffer.Cursor() == len(runes) {
			cursorRow = len(rows) - 1
			cursorColumn = 0
		}
	}

	targetRow := cursorRow + direction
	if targetRow < 0 || targetRow >= len(rows) {
		return false
	}

	buffer.cursor = positionAtColumn(rows[targetRow], runes, cursorColumn)
	return true
}

func positionAtColumn(row width.Row, runes []rune, column int) int {
	position := row.Begin
	for position < row.End && width.Of(string(runes[row.Begin:position+1])) <= column {
		position++
	}

	return position
}

func locate(rows []width.Row, runes []rune, cursor int, room int) (int, int) {
	for i, row := range rows {
		if cursor >= row.End && cursor <= row.Next && row.Next > row.End {
			return min(i+1, len(rows)-1), 0
		}

		if cursor >= row.Begin && cursor <= row.End {
			column := width.Of(string(runes[row.Begin:cursor]))
			if room > 0 {
				column = min(column, room)
			}

			return i, column
		}
	}

	return 0, 0
}
