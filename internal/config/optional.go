package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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

// OptionalInt is an integer config value that records whether it was set, so
// an override can tell "unset, inherit" from an explicit value. An empty env
// var is left unset (caarlos0/env skips it), as is a YAML null.
type OptionalInt struct {
	set   bool
	value int
}

// NewOptionalInt returns a set OptionalInt. Intended for tests and
// programmatic config construction.
func NewOptionalInt(value int) OptionalInt {
	return OptionalInt{set: true, value: value}
}

// Get returns the value and whether it was set.
func (o OptionalInt) Get() (int, bool) {
	return o.value, o.set
}

// Or returns the value if set, otherwise fallback.
func (o OptionalInt) Or(fallback int) int {
	if o.set {
		return o.value
	}
	return fallback
}

func (o *OptionalInt) UnmarshalText(b []byte) error {
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("must be an integer: %q", string(b))
	}
	o.set = true
	o.value = n
	return nil
}

func (o *OptionalInt) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		return nil
	}
	if err := node.Decode(&o.value); err != nil {
		return err
	}
	o.set = true
	return nil
}

// OptionalBool is a boolean config value that records whether it was set, so
// an override can tell "unset, inherit" from an explicit value. An empty env
// var is left unset (caarlos0/env skips it), as is a YAML null.
type OptionalBool struct {
	set   bool
	value bool
}

// NewOptionalBool returns a set OptionalBool. Intended for tests and
// programmatic config construction.
func NewOptionalBool(value bool) OptionalBool {
	return OptionalBool{set: true, value: value}
}

// Get returns the value and whether it was set.
func (o OptionalBool) Get() (bool, bool) {
	return o.value, o.set
}

// Or returns the value if set, otherwise fallback.
func (o OptionalBool) Or(fallback bool) bool {
	if o.set {
		return o.value
	}
	return fallback
}

func (o *OptionalBool) UnmarshalText(b []byte) error {
	v, err := strconv.ParseBool(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("must be a boolean: %q", string(b))
	}
	o.set = true
	o.value = v
	return nil
}

func (o *OptionalBool) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		return nil
	}
	if err := node.Decode(&o.value); err != nil {
		return err
	}
	o.set = true
	return nil
}
