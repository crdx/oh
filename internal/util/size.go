package util

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	sizeKilobyte = 1 << 10
	sizeMegabyte = 1 << 20
	sizeGigabyte = 1 << 30
)

func ParseSize(text string) (int, error) {
	writtenSize := strings.TrimSpace(text)

	multiplier := 1

	switch {
	case strings.HasSuffix(strings.ToUpper(writtenSize), "K"):
		multiplier = sizeKilobyte
	case strings.HasSuffix(strings.ToUpper(writtenSize), "M"):
		multiplier = sizeMegabyte
	case strings.HasSuffix(strings.ToUpper(writtenSize), "G"):
		multiplier = sizeGigabyte
	}

	if multiplier > 1 {
		writtenSize = writtenSize[:len(writtenSize)-1]
	}

	count, err := strconv.Atoi(strings.TrimSpace(writtenSize))
	if err != nil || count < 0 {
		return 0, fmt.Errorf("%q is not a size; write a count of bytes, or one with a K, M or G after it", text)
	}

	if count > math.MaxInt/multiplier {
		return 0, fmt.Errorf("%q is larger than this machine can count", text)
	}

	return count * multiplier, nil
}
