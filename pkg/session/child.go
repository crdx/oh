package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const ChildrenDirectoryName = "subagents"

var ErrNoChildName = errors.New("every subagent name is taken")

func ChildrenDir(directory string, name string) string {
	return filepath.Join(Dir(directory, name), ChildrenDirectoryName)
}

func ChildName(parent string, siblings []string, pick func(int) int) (string, error) {
	adjective, parentAnimal, isNamed := strings.Cut(parent, "-")
	if !isNamed {
		return "", errors.New("the parent session has no adjective to share")
	}

	takenEmojis := []string{animalEmojis[parentAnimal]}
	for _, sibling := range siblings {
		takenEmojis = append(takenEmojis, Emoji(sibling))
	}

	var distinct, available []string
	for _, animal := range animals {
		candidate := adjective + "-" + animal
		if candidate == parent || slices.Contains(siblings, candidate) {
			continue
		}
		available = append(available, candidate)
		if !slices.Contains(takenEmojis, animalEmojis[animal]) {
			distinct = append(distinct, candidate)
		}
	}

	for _, candidates := range [][]string{distinct, available} {
		if len(candidates) > 0 {
			return candidates[pick(len(candidates))], nil
		}
	}

	return "", ErrNoChildName
}

func CreateNamed(directory string, name string, journalMeta []byte, listingData []byte) (*Writer, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if Exists(directory, name) {
		return nil, fmt.Errorf("a session named %s already exists", name)
	}

	return newWriter(directory, name, journalMeta, listingData), nil
}
