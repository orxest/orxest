//go:build !windows

package childproc

import (
	"os"
	"os/exec"
	"syscall"
)

// Signal is a portable alias for the signals Orxest uses to stop an agent.
type Signal = syscall.Signal

const (
	// Interrupt is the graceful stop (Pi treats it as abort).
	Interrupt = syscall.SIGINT
	// Terminate asks the process to exit.
	Terminate = syscall.SIGTERM
	// Kill cannot be ignored.
	Kill = syscall.SIGKILL
)

func configureGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(proc *os.Process, sig Signal) error {
	if err := syscall.Kill(-proc.Pid, sig); err == nil {
		return nil
	}
	return proc.Signal(sig)
}
