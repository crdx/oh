package sim

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
)

type Scenario struct {
	Model string `toml:"model"`

	Wire string `toml:"wire"`

	Loop bool `toml:"loop"`

	Strict bool `toml:"strict"`

	Pace  Duration `toml:"pace"`
	Delay Duration `toml:"delay"`

	Turns []Turn `toml:"turn"`
}

type Turn struct {
	Think []string `toml:"think"`
	Say   string   `toml:"say"`
	Calls []Call   `toml:"call"`

	Pace  Duration `toml:"pace"`
	Delay Duration `toml:"delay"`

	Fail       string `toml:"fail"`
	Incomplete bool   `toml:"incomplete"`
	ErrorEvent string `toml:"error-event"`

	Truncate bool `toml:"truncate"`

	Status     int      `toml:"status"`
	RetryAfter Duration `toml:"retry-after"`
}

type Call struct {
	Name      string `toml:"name"`
	Arguments string `toml:"arguments"`
}

type Duration struct {
	time.Duration
}

func (self *Duration) UnmarshalText(text []byte) error {
	durationValue, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}

	self.Duration = durationValue

	return nil
}

func Read(path string) (*Scenario, error) {
	var self Scenario

	metadata, err := toml.DecodeFile(path, &self)
	if err != nil {
		return nil, err
	}

	if undecodedKeys := metadata.Undecoded(); len(undecodedKeys) > 0 {
		return nil, fmt.Errorf("%s: no such setting: %s", path, undecodedKeys[0])
	}

	if len(self.Turns) == 0 {
		return nil, fmt.Errorf("%s: a scenario needs at least one turn", path)
	}

	if self.Model == "" {
		self.Model = "fake"
	}

	return &self, nil
}

func (self *Scenario) turn(index int) (Turn, bool) {
	if index < len(self.Turns) {
		return self.Turns[index], true
	}

	if self.Loop && len(self.Turns) > 0 {
		return self.Turns[index%len(self.Turns)], true
	}

	return Turn{}, false
}

func (self *Scenario) pace(turn Turn) time.Duration {
	if turn.Pace.Duration > 0 {
		return turn.Pace.Duration
	}

	return self.Pace.Duration
}

func (self *Scenario) delay(turn Turn) time.Duration {
	if turn.Delay.Duration > 0 {
		return turn.Delay.Duration
	}

	return self.Delay.Duration
}
