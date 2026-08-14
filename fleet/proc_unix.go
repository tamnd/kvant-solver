//go:build !windows

package fleet

import (
	"os"
	"os/exec"
	"syscall"
)

// detach puts ssh in its own process group, so it outlives the command that
// started it and a stray Ctrl-C in the terminal does not take the fleet down.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup asks a whole process group to stop. ssh may have children, and
// killing only the leader leaves the forward held open by something with no
// parent. The caller falls back to the single process when this fails.
func killGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// terminate is the ordinary polite stop for one process.
func terminate(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

// running asks whether a pid is still there. Signal 0 is the ordinary way, and
// it answers for a process this shell does not own as long as it is ours.
func running(process *os.Process) bool {
	return process.Signal(syscall.Signal(0)) == nil
}
