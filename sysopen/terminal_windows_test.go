//go:build windows

package sysopen

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsTerminalUsesWorkingDirectoryAndNewConsole(t *testing.T) {
	cmd := terminalCmd(`C:\work & notes`, []string{"tool.cmd", "two words", "a&b"})
	if cmd.Dir != `C:\work & notes` {
		t.Fatalf("terminal cwd = %q", cmd.Dir)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_CONSOLE == 0 {
		t.Fatal("terminal should open in a new Windows console")
	}
	if strings.Contains(strings.ToLower(cmd.SysProcAttr.CmdLine), "cd /d") {
		t.Fatalf("working directory was interpolated into cmd.exe input: %q", cmd.SysProcAttr.CmdLine)
	}
	if !strings.Contains(cmd.SysProcAttr.CmdLine, "a^&b") {
		t.Fatalf("command metacharacter was not escaped: %q", cmd.SysProcAttr.CmdLine)
	}
}
