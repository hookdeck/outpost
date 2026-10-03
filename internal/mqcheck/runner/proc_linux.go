package runner

import (
	"os/exec"
	"syscall"
)

// setParentDeathSignal kills a worker when the driver dies, so consumers never
// outlive a crashed run.
func setParentDeathSignal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
