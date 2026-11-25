package config

import (
	"errors"
	"time"

	"crdx.org/oh/internal/app/tr"
)

type ResyncThreshold struct {
	Duration time.Duration
}

func (self *ResyncThreshold) UnmarshalTOML(value any) error {
	switch configuredValue := value.(type) {
	case bool:
		if !configuredValue {
			self.Duration = 0
			return nil
		}
	case string:
		duration, err := time.ParseDuration(configuredValue)
		if err == nil && duration > 0 {
			self.Duration = duration
			return nil
		}
	}
	return errors.New(tr.DurationOrOffShape)
}
