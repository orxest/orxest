//go:build windows

package childproc

import (
	"os"
	"os/exec"
	"syscall"
)

// Signal is a portable alias for the signals Orxest uses to stop an agent.
type Signal = syscall.Signal

const (
	// Interrupt requests a graceful stop.
	Interrupt = syscall.SIGINT
	// Terminate asks the process to exit.
	Terminate = syscall.SIGTERM
	// Kill cannot be ignored.
	Kill = syscall.SIGKILL
)

// configureGroup is a no-op on Windows, which has no POSIX process groups.
func configureGroup(cmd *exec.Cmd) {}

// signalGroup terminates the agent process: Windows has no portable way to
// signal a whole process tree, so cancellation falls back to killing the agent.
func signalGroup(proc *os.Process, sig Signal) error {
	return proc.Kill()
}
