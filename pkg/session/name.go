package session

import (
	"errors"
	"math/rand/v2"
	"regexp"
)

var namePattern = regexp.MustCompile(`^[a-z]+-[a-z]+$`)

func newName(directory string) (string, error) {
	names, err := StoredNames(directory)
	if err != nil {
		return "", err
	}

	takenNames := make(map[string]bool, len(names))
	for _, name := range names {
		takenNames[name] = true
	}

	total := len(adjectives) * len(animals)
	start := rand.IntN(total) //nolint:gosec // a name must be memorable, not unguessable

	for offset := range total {
		index := (start + offset) % total
		candidate := adjectives[index/len(animals)] + "-" + animals[index%len(animals)]
		if !takenNames[candidate] {
			return candidate, nil
		}
	}

	return "", errors.New("every session name is taken")
}
