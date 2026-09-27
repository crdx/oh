package fit

import (
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/app/style"
)

const (
	separator  = ", "
	hiddenMark = "+"
)

func Ladder(rungs ...string) []string {
	keptRungs := make([]string, 0, len(rungs))

	for _, rung := range rungs {
		if len(keptRungs) > 0 && keptRungs[len(keptRungs)-1] == rung {
			continue
		}

		keptRungs = append(keptRungs, rung)

		if rung == "" {
			break
		}
	}

	return keptRungs
}

func Parts(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}

	rungs := make([]string, 0, len(parts)+2)
	rungs = append(rungs, join(parts))

	for shownCount := range slices.Backward(parts) {
		hiddenCount := len(parts) - shownCount
		shownParts := append(slices.Clone(parts[:shownCount]), hidden(hiddenCount))
		rungs = append(rungs, join(shownParts))
	}

	return Ladder(append(rungs, style.Subtle(hiddenMark))...)
}

func join(parts []string) string {
	return strings.Join(parts, style.Subtle(separator))
}

func hidden(count int) string {
	return style.Subtle(hiddenMark + strconv.Itoa(count))
}
