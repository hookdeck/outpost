// Package destenv reads the settings of the destination helpers in
// cmd/destinations. Defaults point at the local destination stack
// (`make up/dest`), so each helper runs with no settings at all; environment
// variables point it elsewhere or give it names of its own, for example when
// two checkouts share one stack.
package destenv

import "os"

// Get returns the environment variable key, or def when it is unset. A
// variable set to the empty string returns "", so a default can be turned off
// (DEST_AWS_ENDPOINT="" talks to AWS itself). Variables carry a DEST_ prefix so
// they don't pick up the SDKs' or Outpost's own settings (AWS_REGION,
// RABBITMQ_EXCHANGE, ...) from your shell.
func Get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
