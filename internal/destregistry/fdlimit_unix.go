//go:build unix

package destregistry

import "syscall"

// softFDLimit returns the process's soft RLIMIT_NOFILE.
func softFDLimit() (int, bool) {
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		return 0, false
	}
	// RLIM_INFINITY, or any value past int range, is not a useful basis for
	// sizing — fall back rather than overflow.
	if rlim.Cur > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(rlim.Cur), true
}
