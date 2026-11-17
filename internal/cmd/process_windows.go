//go:build windows

package cmd

import "os/exec"

// configureProcess keeps the default cancellation behaviour on windows: the
// context cancellation kills the direct child process.
func configureProcess(_ *exec.Cmd) {}
