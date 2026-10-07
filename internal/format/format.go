package format

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

var (
	ErrNewer = errors.New("written by a newer build than this one")
	ErrOlder = errors.New("written by an older build than this one")
)

func Check(found int, supportedFormat int) error {
	if found > supportedFormat {
		return fmt.Errorf("format %d was %w (this one reads format %d)", found, ErrNewer, supportedFormat)
	}

	return nil
}

func Require(found int, supportedFormat int) error {
	if found < supportedFormat {
		return fmt.Errorf("format %d was %w (this one reads format %d)", found, ErrOlder, supportedFormat)
	}

	return Check(found, supportedFormat)
}

func IsNewer(err error) bool {
	return errors.Is(err, ErrNewer)
}

func IsOlder(err error) bool {
	return errors.Is(err, ErrOlder)
}

type header struct {
	Version int `json:"version" toml:"version"`
}

func ReadJSON(data []byte) (int, error) {
	var read header
	if err := json.Unmarshal(data, &read); err != nil {
		return 0, err
	}

	return read.Version, nil
}

func ReadTOML(data []byte) (int, error) {
	var read header
	if _, err := toml.Decode(string(data), &read); err != nil {
		return 0, err
	}

	return read.Version, nil
}
