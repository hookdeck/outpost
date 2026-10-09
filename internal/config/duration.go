package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration config value written as a Go duration string
// ("90s", "5m", "24h") or a whole number of seconds ("3600"), both in the
// environment and in YAML. A plain time.Duration field accepts neither a bare
// number from the environment nor a YAML integer.
//
// A YAML null leaves the value unset, like an absent key. caarlos0/env skips a
// present-but-empty env var, so the default stays.
type Duration time.Duration

// Duration returns the value as a time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String formats the value like time.Duration ("1h0m0s").
func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalText(b []byte) error {
	parsed, err := parseDuration(string(b))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: a duration must be a string such as \"90s\" or a number of seconds", node.Line)
	}
	parsed, err := parseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*d = parsed
	return nil
}

// maxDurationSeconds is the largest whole number of seconds a time.Duration
// holds.
const maxDurationSeconds = math.MaxInt64 / int64(time.Second)

// parseDuration parses a whole number of seconds, else a Go duration string.
func parseDuration(s string) (Duration, error) {
	s = strings.TrimSpace(s)
	if seconds, err := strconv.ParseInt(s, 10, 64); err == nil {
		if seconds > maxDurationSeconds || seconds < -maxDurationSeconds {
			return 0, fmt.Errorf("duration %q is out of range", s)
		}
		return Duration(time.Duration(seconds) * time.Second), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: use a Go duration such as \"90s\", \"5m\" or \"24h\", or a whole number of seconds", s)
	}
	return Duration(d), nil
}
