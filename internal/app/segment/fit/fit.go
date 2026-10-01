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
	return Shown(len(parts), func(shownCount int) []string { return parts[:shownCount] })
}

func Shown(count int, partsShowing func(int) []string) []string {
	if count == 0 {
		return nil
	}

	rungs := make([]string, 0, count+2)
	rungs = append(rungs, join(partsShowing(count)))

	for shownCount := count - 1; shownCount >= 0; shownCount-- {
		shownParts := append(slices.Clone(partsShowing(shownCount)), hidden(count-shownCount))
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
