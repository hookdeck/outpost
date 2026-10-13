//go:build !linux

package runner

import "os/exec"

func setParentDeathSignal(cmd *exec.Cmd) {}
