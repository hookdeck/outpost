//go:build windows

package main

import (
	"os"
	"os/exec"
)

// Windows has no exec(2) equivalent, so the child-process form is kept here.
// Release builds are linux-only; this exists so the repo still builds on
// Windows for development.
func execBinary(binary string, args []string) error {
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
