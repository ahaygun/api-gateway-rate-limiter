package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from human-friendly YAML
// strings such as "5s" or "200ms" (plain time.Duration would require an
// integer count of nanoseconds).
type Duration time.Duration

// UnmarshalYAML parses a duration string like "5s" into a Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"5s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std returns the value as a standard library time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }
