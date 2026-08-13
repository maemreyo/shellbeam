package config

import (
	"bytes"
	"errors"
	"github.com/pelletier/go-toml/v2"
	"os"
)

type Overrides struct {
	RuntimeDir            *string
	StateDir              *string
	Shell                 *string
	MaxConcurrentSessions *int
}

func Load(path string, overrides Overrides) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if err == nil {
		if err = toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&c); err != nil {
			return Config{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	if overrides.RuntimeDir != nil {
		c.RuntimeDir = *overrides.RuntimeDir
	}
	if overrides.StateDir != nil {
		c.StateDir = *overrides.StateDir
	}
	if overrides.Shell != nil {
		c.Shell = *overrides.Shell
	}
	if overrides.MaxConcurrentSessions != nil {
		c.MaxConcurrentSessions = *overrides.MaxConcurrentSessions
	}
	return c, c.Validate()
}
