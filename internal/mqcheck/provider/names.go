package provider

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// RunName returns a run id that carries its creation time, so Sweep can tell
// a crashed run's leftovers from a live run's resources: "<base36 unix
// seconds><4 random chars>".
func RunName(now time.Time) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return strconv.FormatInt(now.Unix(), 36) + string(b)
}

// ResourceName is the base name of a run's resource: "<prefix>-<run>-<what>".
// Providers append their own suffixes ("-dlq", "-sub").
func ResourceName(prefix, run, what string) string {
	return prefix + "-" + run + "-" + what
}

// ShouldSweep reports whether Sweep should delete the resource named name:
// it is "<prefix>-<run id>-..." with prefix exactly as given to Sweep (no
// trailing "-"), the run id made by RunName, and the run is older than
// olderThan (0 = any age). Anything else (another prefix that starts the same
// way, a name without a run id) is skipped. Every provider's Sweep uses it,
// so the guard is the same everywhere.
func ShouldSweep(name, prefix string, olderThan time.Duration) bool {
	t, ok := NameTime(name, prefix)
	if !ok {
		return false
	}
	return olderThan <= 0 || time.Since(t) >= olderThan
}

// NameTime returns the creation time encoded in a resource name
// "<prefix>-<run id>-...", where the run id was made with RunName. ok is
// false for any other name.
func NameTime(name, prefix string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, prefix+"-")
	if !ok {
		return time.Time{}, false
	}
	run, _, found := strings.Cut(rest, "-")
	if !found || len(run) != 10 {
		return time.Time{}, false
	}
	for _, c := range run {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return time.Time{}, false
		}
	}
	secs, err := strconv.ParseInt(run[:6], 36, 64)
	if err != nil {
		return time.Time{}, false
	}
	t := time.Unix(secs, 0)
	if t.Year() < 2020 || t.After(time.Now().Add(24*time.Hour)) {
		return time.Time{}, false
	}
	return t, true
}
