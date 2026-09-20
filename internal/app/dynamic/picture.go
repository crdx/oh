package dynamic

import (
	"crdx.org/oh/internal/app/graphics"
)

type Picture struct {
	Path   string
	Data   []byte
	Width  int
	Height int

	CellWidth  int
	CellHeight int
	IsLocal    bool
}

type pictureRows struct {
	picture Picture
	columns int
	rows    []string
}

func (self *Block) AttachPicture(rowIndex int, picture Picture) {
	if picture.Width <= 0 || picture.Height <= 0 {
		return
	}

	self.change(func() {
		if rowIndex < 0 || rowIndex >= len(self.rows) {
			return
		}

		self.rows[rowIndex].picture = &pictureRows{picture: picture}
	})
}

func (self *pictureRows) render(columns int) []string {
	if self.rows != nil && self.columns == columns {
		return self.rows
	}

	self.columns = columns
	self.rows = self.place(columns)

	return self.rows
}

func (self *pictureRows) place(columns int) []string {
	box, isDrawable := self.picture.box(columns)
	if !isDrawable {
		return []string{}
	}

	if self.picture.IsLocal {
		if rows, isPlaced := graphics.PlaceFile(self.picture.Path, box); isPlaced {
			return rows
		}
	}

	if rows, isPlaced := graphics.PlacePNG(self.picture.Data, box); isPlaced {
		return rows
	}

	return []string{}
}

func (self Picture) box(columns int) (graphics.Box, bool) {
	return graphics.Fit(
		graphics.Size{Width: self.Width, Height: self.Height},
		graphics.Size{Width: self.CellWidth, Height: self.CellHeight},
		columns,
	)
}
