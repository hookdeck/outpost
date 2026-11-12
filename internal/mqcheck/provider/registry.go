package provider

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = map[string]Registration{}
)

// Register adds a provider. Provider files call it from init(). It panics on
// a duplicate or incomplete registration, so mistakes fail at start-up.
func Register(r Registration) {
	if r.Name == "" || r.Open == nil {
		panic("mqcheck: provider registration needs Name and Open")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[r.Name]; dup {
		panic(fmt.Sprintf("mqcheck: provider %q registered twice", r.Name))
	}
	registry[r.Name] = r
}

// Lookup returns the provider registered as name.
func Lookup(name string) (Registration, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := registry[name]
	return r, ok
}

// All returns every registered provider, sorted by name.
func All() []Registration {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Registration, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// NewConfig returns the Config for r: settings from the environment, falling
// back to their defaults.
func NewConfig(r Registration, prefix string) Config {
	defaults := map[string]string{}
	for _, s := range r.Settings {
		defaults[s.Env] = s.Default
	}
	return Config{
		Prefix: prefix,
		Get: func(env string) string {
			if v, ok := os.LookupEnv(env); ok && v != "" {
				return v
			}
			return defaults[env]
		},
	}
}
