//go:build !unix

package destregistry

// softFDLimit has no portable equivalent outside Unix. Callers fall back to a
// conservative assumed limit.
func softFDLimit() (int, bool) {
	return 0, false
}
