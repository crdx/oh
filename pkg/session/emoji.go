package session

import "strings"

func Emoji(name string) string {
	_, animal, found := strings.Cut(name, "-")
	if !found {
		return ""
	}

	return animalEmojis[animal]
}
