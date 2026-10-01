package edit

import (
	"slices"

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
	laid := withCursorRow(width.Rows(string(runes), room), buffer.Cursor(), room)

	rows := make([]string, 0, len(laid))
	for _, row := range laid {
		rows = append(rows, row.Text)
	}

	cursorRow, cursorColumn := locate(laid, runes, buffer.Cursor(), room)

	return rows, cursorRow, cursorColumn
}

func withCursorRow(rows []width.Row, cursor int, room int) []width.Row {
	for i, row := range rows {
		if !isContinued(rows, i) && isPastRow(row, cursor, room) {
			return slices.Insert(rows, i+1, width.Row{Begin: row.Next, End: row.Next, Next: row.Next})
		}
	}

	return rows
}

func isPastRow(row width.Row, cursor int, room int) bool {
	if cursor < row.End || cursor > row.Next {
		return false
	}

	return cursor > row.End || row.Next > row.End || room > 0 && width.Of(row.Text) >= room
}

func isContinued(rows []width.Row, i int) bool {
	return i+1 < len(rows) && rows[i+1].Begin == rows[i].Next
}

func moveCursorVertically(buffer *Buffer, room int, direction int) bool {
	runes := buffer.Runes()
	rows := width.Rows(string(runes), room)
	rows = withCursorRow(withCursorRow(rows, len(runes), room), buffer.Cursor(), room)
	cursorRow, cursorColumn := locate(rows, runes, buffer.Cursor(), room)

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
		if cursor < row.Begin || cursor > row.Next {
			continue
		}

		if isContinued(rows, i) && isPastRow(row, cursor, room) {
			return i + 1, 0
		}

		column := width.Of(string(runes[row.Begin:min(cursor, row.End)]))
		if room > 0 {
			column = min(column, room)
		}

		return i, column
	}

	return 0, 0
}
