//go:build !windows

package cmd

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureProcess puts the command into its own process group and kills the
// whole group on cancellation. Without this, orphaned grandchild processes
// (e.g. a command spawned by a shell) keep the output pipes open and
// CombinedOutput never returns after a context cancellation.
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		//negative pid kills the whole process group
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
}
