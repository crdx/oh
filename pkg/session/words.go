package session

import (
	_ "embed"
	"encoding/json"
	"slices"
)

type animalCharacter struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

type characterSet struct {
	Adjectives     []string          `json:"adjectives"`
	Animals        []animalCharacter `json:"animals"`
	RetiredAnimals []animalCharacter `json:"retired"`
}

//go:embed characters.json
var characterData []byte

var (
	characters   = decodeCharacters(characterData)
	adjectives   = characters.Adjectives
	animals      = getAnimalNames(characters.Animals)
	animalEmojis = getAnimalEmojis(characters.Animals, characters.RetiredAnimals)
)

func decodeCharacters(data []byte) characterSet {
	var characters characterSet
	if err := json.Unmarshal(data, &characters); err != nil {
		panic(err)
	}
	return characters
}

func getAnimalNames(characterAnimals []animalCharacter) []string {
	names := make([]string, len(characterAnimals))
	for i, animal := range characterAnimals {
		names[i] = animal.Name
	}
	return names
}

func getAnimalEmojis(namedAnimals []animalCharacter, retiredAnimals []animalCharacter) map[string]string {
	emojis := make(map[string]string, len(namedAnimals)+len(retiredAnimals))
	for _, animal := range slices.Concat(namedAnimals, retiredAnimals) {
		emojis[animal.Name] = animal.Emoji
	}
	return emojis
}
