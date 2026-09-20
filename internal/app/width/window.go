package width

const VerticalEllipsis = "⋮"

type Window struct {
	Rows             []string
	Focus            int
	HiddenLinesAbove int
	HiddenLinesBelow int
}

func WindowRows(rows []string, budget int, focus int) Window {
	budget = max(budget, 1)

	if len(rows) <= budget {
		return Window{Rows: rows, Focus: focus}
	}

	start := min(max(focus-budget+1, 0), len(rows)-budget)

	return Window{
		Rows:             rows[start : start+budget],
		Focus:            focus - start,
		HiddenLinesAbove: start,
		HiddenLinesBelow: len(rows) - start - budget,
	}
}
