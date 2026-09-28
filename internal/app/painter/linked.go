package painter

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
