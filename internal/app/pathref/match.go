package pathref

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

const (
	matchScore       = 16
	boundaryBonus    = 8
	separatorBonus   = 10
	consecutiveBonus = 12
	basenameBonus    = 4
	gapPenalty       = 1
	gapOpenPenalty   = 3
	maxGapPenalty    = 12
)

type candidate struct {
	index int
	score int
}

type Matcher struct {
	listing         *Listing
	query           string
	matchingIndices []int
}

func (self *Matcher) Match(listing *Listing, query string, limit int) ([]string, int) {
	if listing == nil {
		return nil, 0
	}

	isCaseSensitive := strings.IndexFunc(query, unicode.IsUpper) >= 0
	texts := listing.foldedPaths
	if isCaseSensitive {
		texts = listing.Paths
	}

	pool := self.pool(listing, query)
	candidates := make([]candidate, 0, min(len(pool), 1024))
	matchingIndices := make([]int, 0, min(len(pool), 1024))
	for _, index := range pool {
		score, isMatch := score(texts[index], listing.Paths[index], query)
		if !isMatch {
			continue
		}
		matchingIndices = append(matchingIndices, index)
		if listing.Paths[index] == query && IsDirectory(query) {
			continue
		}
		candidates = append(candidates, candidate{index: index, score: score})
	}

	self.listing = listing
	self.query = query
	self.matchingIndices = matchingIndices

	best := bestOf(listing, candidates, limit)
	paths := make([]string, len(best))
	for i, found := range best {
		paths[i] = listing.Paths[found.index]
	}

	return paths, len(candidates)
}

func (self *Matcher) pool(listing *Listing, query string) []int {
	if self.listing == listing && self.matchingIndices != nil && strings.HasPrefix(query, self.query) &&
		isFoldingStable(self.query, query) {
		return self.matchingIndices
	}

	return listing.every
}

func isFoldingStable(previous string, query string) bool {
	hasUpper := func(text string) bool { return strings.IndexFunc(text, unicode.IsUpper) >= 0 }
	return hasUpper(previous) == hasUpper(query)
}

func bestOf(listing *Listing, candidates []candidate, limit int) []candidate {
	compare := func(left candidate, right candidate) int {
		return cmp.Or(
			cmp.Compare(right.score, left.score),
			cmp.Compare(listing.depths[left.index], listing.depths[right.index]),
			strings.Compare(listing.foldedPaths[left.index], listing.foldedPaths[right.index]),
			strings.Compare(listing.Paths[left.index], listing.Paths[right.index]),
		)
	}

	if len(candidates) <= limit {
		slices.SortFunc(candidates, compare)
		return candidates
	}

	best := make([]candidate, 0, limit+1)
	for _, next := range candidates {
		if len(best) == limit && compare(next, best[limit-1]) >= 0 {
			continue
		}
		at, _ := slices.BinarySearchFunc(best, next, compare)
		best = slices.Insert(best, at, next)
		if len(best) > limit {
			best = best[:limit]
		}
	}

	return best
}

func Score(text string, query string) (int, bool) {
	isCaseSensitive := strings.IndexFunc(query, unicode.IsUpper) >= 0
	if isCaseSensitive {
		return score(text, text, query)
	}

	return score(fold(text), text, query)
}

func fold(text string) string {
	lowerCaseText := strings.ToLower(text)
	if len(lowerCaseText) != len(text) {
		return text
	}

	return lowerCaseText
}

func score(text string, original string, query string) (int, bool) {
	if query == "" {
		return 0, true
	}

	end := forwardEnd(text, query)
	if end < 0 {
		return 0, false
	}

	start := backwardStart(text, query, end)
	basenameStart := strings.LastIndexByte(strings.TrimSuffix(text, "/"), '/') + 1

	total := 0
	previous := -1
	position := start
	for i := range len(query) {
		for text[position] != query[i] {
			position++
		}

		total += matchScore + boundaryBonusAt(original, position)
		if position >= basenameStart {
			total += basenameBonus
		}
		if previous >= 0 {
			if gap := position - previous - 1; gap == 0 {
				total += consecutiveBonus
			} else {
				total -= gapOpenPenalty + min(gap*gapPenalty, maxGapPenalty)
			}
		}

		previous = position
		position++
	}

	return total, true
}

func forwardEnd(text string, query string) int {
	matchedCount := 0
	for position := range len(text) {
		if text[position] != query[matchedCount] {
			continue
		}
		matchedCount++
		if matchedCount == len(query) {
			return position
		}
	}

	return -1
}

func backwardStart(text string, query string, end int) int {
	queryIndex := len(query) - 1
	for position := end; position >= 0; position-- {
		if text[position] != query[queryIndex] {
			continue
		}
		if queryIndex == 0 {
			return position
		}
		queryIndex--
	}

	return 0
}

func boundaryBonusAt(text string, position int) int {
	if position == 0 {
		return separatorBonus
	}

	switch text[position-1] {
	case '/':
		return separatorBonus
	case '_', '-', '.', ' ':
		return boundaryBonus
	}

	if isLower(text[position-1]) && isUpper(text[position]) {
		return boundaryBonus
	}

	return 0
}

func isLower(character byte) bool {
	return character >= 'a' && character <= 'z'
}

func isUpper(character byte) bool {
	return character >= 'A' && character <= 'Z'
}
