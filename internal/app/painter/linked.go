package painter

import "crdx.org/oh/internal/app/link"

type rowLinks struct {
	sources []string
	results []string
	roots   link.Roots
}

func (self *rowLinks) Reset() {
	self.sources = nil
	self.results = nil
}

func (self *rowLinks) Render(rows []string, roots link.Roots) []string {
	if roots != self.roots {
		self.Reset()
		self.roots = roots
	}

	results := make([]string, len(rows))
	for i, row := range rows {
		if i < len(self.sources) && self.sources[i] == row {
			results[i] = self.results[i]
			continue
		}

		results[i] = link.Render(row, roots)
	}

	self.sources = append(self.sources[:0], rows...)
	self.results = append(self.results[:0], results...)

	return results
}
