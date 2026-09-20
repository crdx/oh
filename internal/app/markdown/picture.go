package markdown

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

func (self *renderer) picture(node ast.Node) bool {
	if self.pictures == nil {
		return false
	}

	picture, isSolePicture := self.solePicture(node)
	if !isSolePicture {
		return false
	}

	rows, isDrawn := self.pictures.DrawPicture(string(picture.Destination), self.columns)
	if !isDrawn {
		return false
	}

	self.rows = append(self.rows, rows...)

	return true
}

func (self *renderer) solePicture(node ast.Node) (*ast.Image, bool) {
	var picture *ast.Image

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch child := child.(type) {
		case *ast.Image:
			if picture != nil {
				return nil, false
			}

			picture = child

		case *ast.Text:
			if strings.TrimSpace(self.text(child)) != "" {
				return nil, false
			}

		default:
			return nil, false
		}
	}

	return picture, picture != nil
}
