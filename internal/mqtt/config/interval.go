package config

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

type Interval time.Duration

// UnmarshalYAML accepts a duration string like "10s", quoted or unquoted.
func (i *Interval) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	duration, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	*i = Interval(duration)
	return nil
}
