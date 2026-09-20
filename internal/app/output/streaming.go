package output

import (
	"fmt"
	"slices"
	"strings"
)

type StreamingMode int

const (
	StreamingModeLine StreamingMode = iota

	StreamingModeASAP

	StreamingModePaced
)

var streamingModes = map[string]StreamingMode{
	"asap":  StreamingModeASAP,
	"line":  StreamingModeLine,
	"paced": StreamingModePaced,
}

func (self *StreamingMode) UnmarshalTOML(value any) error {
	name, isText := value.(string)
	if !isText {
		return fmt.Errorf("stream is not one of %s", strings.Join(streamingModeNames(), ", "))
	}

	setting, isKnown := streamingModes[strings.TrimSpace(name)]
	if !isKnown {
		return fmt.Errorf(
			"stream must be one of %s (got %q)",
			strings.Join(streamingModeNames(), ", "), name,
		)
	}

	*self = setting

	return nil
}

func streamingModeNames() []string {
	names := make([]string, 0, len(streamingModes))
	for name := range streamingModes {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}
