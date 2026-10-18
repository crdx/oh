package config

import "crdx.org/oh/internal/util"

type Size struct {
	Bytes int
}

func (self *Size) UnmarshalText(text []byte) error {
	count, err := util.ParseSize(string(text))
	if err != nil {
		return err
	}

	self.Bytes = count

	return nil
}
