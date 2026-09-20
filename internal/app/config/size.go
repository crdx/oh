package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	kilobyte = 1 << 10
	megabyte = 1 << 20
	gigabyte = 1 << 30
)

type Size struct {
	Bytes int
}

func (self *Size) UnmarshalText(text []byte) error {
	writtenSize := strings.TrimSpace(string(text))

	multiplier := 1

	switch {
	case strings.HasSuffix(strings.ToUpper(writtenSize), "K"):
		multiplier = kilobyte
	case strings.HasSuffix(strings.ToUpper(writtenSize), "M"):
		multiplier = megabyte
	case strings.HasSuffix(strings.ToUpper(writtenSize), "G"):
		multiplier = gigabyte
	}

	if multiplier > 1 {
		writtenSize = writtenSize[:len(writtenSize)-1]
	}

	count, err := strconv.Atoi(strings.TrimSpace(writtenSize))
	if err != nil || count < 0 {
		return fmt.Errorf("%q is not a size; write a count of bytes, or one with a K, M or G after it", text)
	}

	if count > math.MaxInt/multiplier {
		return fmt.Errorf("%q is larger than this machine can count", text)
	}

	self.Bytes = count * multiplier

	return nil
}
