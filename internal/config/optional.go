package config

import "gopkg.in/yaml.v3"

// OptionalString is a config value that records whether it was provided at all,
// distinguishing "unset" from "explicitly set to empty string". This lets a
// single field express three states — unset, empty, value — which a plain
// string cannot.
//
// It implements both encoding.TextUnmarshaler (so caarlos0/env binds it as a
// scalar — avoiding the pointer-recursion crash a *string triggers) and
// yaml.Unmarshaler (so YAML can express the empty state via `key: ""`).
//
// One gap remains: caarlos0/env ignores a present-but-empty env var entirely and
// never invokes UnmarshalText for it, so the empty-env case is handled
// explicitly during parsing via OSInterface.LookupEnv (see captureEmptyEnv).
type OptionalString struct {
	set   bool
	value string
}

// NewOptionalString returns a set OptionalString. Intended for tests and
// programmatic config construction.
func NewOptionalString(value string) OptionalString {
	return OptionalString{set: true, value: value}
}

// Get returns the value and whether it was set.
func (o OptionalString) Get() (string, bool) {
	return o.value, o.set
}

func (o *OptionalString) UnmarshalText(b []byte) error {
	o.set = true
	o.value = string(b)
	return nil
}

func (o *OptionalString) UnmarshalYAML(node *yaml.Node) error {
	// A bare `key:` (no value) is YAML null — treat as unset, matching an absent key.
	if node.Tag == "!!null" {
		return nil
	}
	o.set = true
	return node.Decode(&o.value)
}
