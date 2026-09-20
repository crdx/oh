package er

import (
	"strings"

	"crdx.org/oh/internal/mermaid/runewidth"
)

type glyphs struct {
	h, v, tl, tr, bl, br, teeD, teeU, teeL, teeR, cross rune
	hd, vd                                              rune
}

var (
	unicodeGlyphs = glyphs{'─', '│', '┌', '┐', '└', '┘', '┬', '┴', '┤', '├', '┼', '┈', '┊'}
	asciiGlyphs   = glyphs{'-', '|', '+', '+', '+', '+', '+', '+', '+', '+', '+', '.', ':'}
)

func renderEntity(entity *Entity, glyphSet glyphs, minInner int) []string {
	if len(entity.Attributes) == 0 {
		inner := max(runewidth.StringWidth(entity.Display)+2, minInner)
		pad := inner - runewidth.StringWidth(entity.Display)
		return []string{
			string(glyphSet.tl) + strings.Repeat(string(glyphSet.h), inner) + string(glyphSet.tr),
			string(glyphSet.v) + strings.Repeat(" ", pad/2) + entity.Display +
				strings.Repeat(" ", pad-pad/2) + string(glyphSet.v),
			string(glyphSet.bl) + strings.Repeat(string(glyphSet.h), inner) + string(glyphSet.br),
		}
	}

	rows := make([][4]string, len(entity.Attributes))
	has := [4]bool{true, true, false, false}
	for i, a := range entity.Attributes {
		rows[i] = [4]string{a.Type, a.Name, strings.Join(a.Keys, ","), a.Comment}
		if rows[i][2] != "" {
			has[2] = true
		}
		if rows[i][3] != "" {
			has[3] = true
		}
	}

	var cols []int
	for c := range 4 {
		if has[c] {
			cols = append(cols, c)
		}
	}
	width := map[int]int{}
	for _, c := range cols {
		for _, r := range rows {
			if w := runewidth.StringWidth(r[c]); w > width[c] {
				width[c] = w
			}
		}
	}

	inner := 0
	for i, c := range cols {
		inner += width[c] + 2
		if i > 0 {
			inner++
		}
	}
	if need := max(runewidth.StringWidth(entity.Display)+2, minInner); need > inner && len(cols) > 0 {
		width[cols[len(cols)-1]] += need - inner
		inner = need
	}

	pad := func(s string, w int) string {
		return " " + s + strings.Repeat(" ", w-runewidth.StringWidth(s)) + " "
	}
	rule := func(left, mid, right rune) string {
		var builder strings.Builder
		builder.WriteRune(left)
		for i, c := range cols {
			if i > 0 {
				builder.WriteRune(mid)
			}
			builder.WriteString(strings.Repeat(string(glyphSet.h), width[c]+2))
		}
		builder.WriteRune(right)
		return builder.String()
	}

	var out []string
	out = append(out, string(glyphSet.tl)+strings.Repeat(string(glyphSet.h), inner)+string(glyphSet.tr))
	namePad := inner - runewidth.StringWidth(entity.Display)
	out = append(out, string(glyphSet.v)+strings.Repeat(" ", namePad/2)+entity.Display+
		strings.Repeat(" ", namePad-namePad/2)+string(glyphSet.v))
	out = append(out, rule(glyphSet.teeR, glyphSet.teeD, glyphSet.teeL))
	for _, row := range rows {
		var builder strings.Builder
		builder.WriteRune(glyphSet.v)
		for i, c := range cols {
			if i > 0 {
				builder.WriteRune(glyphSet.v)
			}
			builder.WriteString(pad(row[c], width[c]))
		}
		builder.WriteRune(glyphSet.v)
		out = append(out, builder.String())
	}
	out = append(out, rule(glyphSet.bl, glyphSet.teeU, glyphSet.br))
	return out
}

func Render(diagram *ErDiagram, shouldUseAscii bool) string {
	glyphSet := unicodeGlyphs
	if shouldUseAscii {
		glyphSet = asciiGlyphs
	}
	lay := placeEntities(diagram, glyphSet)

	c := &canvas{}
	for _, p := range lay.placedEntities {
		c.stamp(p.x, p.y, p.lines)
	}
	drawConnectors(c, lay, diagram, glyphSet)
	return c.string()
}
