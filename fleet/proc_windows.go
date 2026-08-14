//go:build windows

package fleet

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP is the nearest thing Windows has to a new process
// group of our own, and it is what keeps a console Ctrl-C from reaching ssh.
const createNewProcessGroup = 0x00000200

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// killGroup has no Windows equivalent worth having. A process group here cannot
// be signalled the way it can on Unix, so this always fails and the caller does
// the single process instead. That is a real difference and not a stub: an ssh
// with children on Windows may leave one behind.
func killGroup(int) error {
	return fmt.Errorf("no process groups on windows")
}

func terminate(process *os.Process) error {
	return process.Kill()
}

func running(process *os.Process) bool {
	// Signal 0 is not implemented on Windows, and FindProcess there only
	// succeeds for a process that exists, so the lookup is the whole answer.
	return process != nil
}
