package graphics

import (
	"encoding/base64"
	"strconv"
	"strings"
)

const MaxRows = 64

var rowMarks = [MaxRows]rune{
	0x305, 0x30d, 0x30e, 0x310, 0x312, 0x33d, 0x33e, 0x33f,
	0x346, 0x34a, 0x34b, 0x34c, 0x350, 0x351, 0x352, 0x357,
	0x35b, 0x363, 0x364, 0x365, 0x366, 0x367, 0x368, 0x369,
	0x36a, 0x36b, 0x36c, 0x36d, 0x36e, 0x36f, 0x483, 0x484,
	0x485, 0x486, 0x487, 0x592, 0x593, 0x594, 0x595, 0x597,
	0x598, 0x599, 0x59c, 0x59d, 0x59e, 0x59f, 0x5a0, 0x5a1,
	0x5a8, 0x5a9, 0x5ab, 0x5ac, 0x5af, 0x5c4, 0x610, 0x611,
	0x612, 0x613, 0x614, 0x615, 0x616, 0x617, 0x657, 0x658,
}

const (
	formatPixels = "32"
	formatPNG    = "100"

	mediumDirect = "d"
	mediumFile   = "f"
)

type Box struct {
	Cells int
	Rows  int
}

type Size struct {
	Width  int
	Height int
}

func Fit(picture Size, cell Size, columns int) (Box, bool) {
	if columns <= 0 || picture.Width <= 0 || picture.Height <= 0 || cell.Width <= 0 || cell.Height <= 0 {
		return Box{}, false
	}

	cells := min((picture.Width+cell.Width-1)/cell.Width, columns)
	if cells <= 0 {
		return Box{}, false
	}

	scaledHeight := picture.Height * cells * cell.Width / picture.Width
	rows := max((scaledHeight+cell.Height-1)/cell.Height, 1)

	if rows > MaxRows {
		rows = MaxRows
		cells = max(picture.Width*rows*cell.Height/(picture.Height*cell.Width), 1)
		cells = min(cells, columns)
	}

	return Box{Cells: cells, Rows: rows}, true
}

func (self Box) isDrawable() bool {
	return self.Cells > 0 && self.Rows > 0 && self.Rows <= MaxRows
}

func PlacePNG(data []byte, box Box) ([]string, bool) {
	if len(data) == 0 {
		return nil, false
	}

	return place(formatPNG, mediumDirect, base64.StdEncoding.EncodeToString(data), box)
}

func PlaceFile(path string, box Box) ([]string, bool) {
	if path == "" {
		return nil, false
	}

	return place(formatPNG, mediumFile, base64.StdEncoding.EncodeToString([]byte(path)), box)
}

func place(format string, medium string, payload string, box Box) ([]string, bool) {
	if !box.isDrawable() {
		return nil, false
	}

	imageID := nextImageID()
	rows := make([]string, box.Rows)

	for row := range box.Rows {
		rows[row] = placementRow(imageID, box.Cells, row)
	}

	rows[0] = transmission(imageID, format, medium, payload, box) + rows[0]

	return rows, true
}

func transmission(imageID int, format string, medium string, payload string, box Box) string {
	var command strings.Builder

	for chunk, isLast := range chunks(payload) {
		command.WriteString(openCommand)

		if chunk.isFirst {
			command.WriteString(strings.Join([]string{
				"a=T", "U=1", "q=2",
				"f=" + format,
				"t=" + medium,
				"i=" + strconv.Itoa(imageID),
				"c=" + strconv.Itoa(box.Cells),
				"r=" + strconv.Itoa(box.Rows),
			}, ","))
			command.WriteString(",")
		}

		command.WriteString("m=")
		command.WriteString(boolDigit(!isLast))
		command.WriteString(";")
		command.WriteString(chunk.text)
		command.WriteString(closeCommand)
	}

	return command.String()
}

func placementRow(imageID int, cells int, row int) string {
	var placement strings.Builder

	placement.WriteString(identifyingColour(imageID))
	placement.WriteString(placeholder)
	placement.WriteRune(rowMarks[row])
	placement.WriteRune(rowMarks[0])
	placement.WriteString(strings.Repeat(placeholder, cells-1))
	placement.WriteString("\x1b[39m")

	return placement.String()
}
