//go:build !windows

package sysopen

import "os/exec"

// Kept on non-Windows hosts so the platform-independent builder tests can inspect the
// argv. buildTerminalCmd only reaches this function when runtime.GOOS is windows.
func windowsTerminal(_ string, command []string) *exec.Cmd {
	argv := windowsTerminalArgs(command)
	return exec.Command(argv[0], argv[1:]...)
}
