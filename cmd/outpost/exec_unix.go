//go:build !windows

package main

import (
	"os"
	"syscall"
)

// execBinary replaces the current process with the target binary rather than
// running it as a child. Spawning would keep this process, and its Go runtime,
// resident for the lifetime of the server. It also makes signals reach the
// server directly instead of being delivered to a wrapper that ignores them.
func execBinary(binary string, args []string) error {
	return syscall.Exec(binary, append([]string{binary}, args...), os.Environ())
}
