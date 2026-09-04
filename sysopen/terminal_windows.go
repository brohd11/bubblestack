//go:build windows

package sysopen

import (
	"os/exec"
	"syscall"

	"github.com/brohd11/goutil/executil"
	"golang.org/x/sys/windows"
)

func windowsTerminal(_ string, command []string) *exec.Cmd {
	argv := windowsTerminalArgs(command)
	cmd := exec.Command(argv[0], argv[1:]...)
	line := syscall.EscapeArg(argv[0]) + " /d /v:off /k"
	if len(command) > 0 {
		inner, err := executil.CmdJoin(command)
		if err != nil {
			cmd.Err = err
			return cmd
		}
		line += " " + inner
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       line,
		CreationFlags: windows.CREATE_NEW_CONSOLE,
	}
	return cmd
}
