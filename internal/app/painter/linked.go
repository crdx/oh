package painter

import "crdx.org/oh/internal/app/link"

type rowMemory struct {
	sources []string
	results []string
}

func (self *rowMemory) Reset() {
	self.sources = nil
	self.results = nil
}

func (self *rowMemory) Render(rows []string, render func(string) string) []string {
	results := make([]string, len(rows))
	for i, row := range rows {
		if i < len(self.sources) && self.sources[i] == row {
			results[i] = self.results[i]
			continue
		}

		results[i] = render(row)
	}

	self.sources = append(self.sources[:0], rows...)
	self.results = append(self.results[:0], results...)

	return results
}

type rowLinks struct {
	memory rowMemory
	roots  link.Roots
}

func (self *rowLinks) Reset() {
	self.memory.Reset()
}

func (self *rowLinks) Render(rows []string, roots link.Roots) []string {
	if roots != self.roots {
		self.Reset()
		self.roots = roots
	}

	return self.memory.Render(rows, func(row string) string {
		return link.Render(row, roots)
	})
}
