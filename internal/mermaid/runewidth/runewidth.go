package runewidth

import "crdx.org/oh/internal/app/width"

func StringWidth(text string) int {
	return width.Of(text)
}

func Cells(text string) []string {
	return width.Cells(text)
}
