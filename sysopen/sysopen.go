// Package sysopen hands paths, URLs and directories to the OS: file manager (Path),
// browser (URL), terminal window (Terminal) or an inline shell on this tty
// (TerminalInline). On Linux every launch sets cmd.Dir to the target, because some
// emulators and wrappers drop their working-directory option.
package sysopen

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/goutil/executil"
	"github.com/brohd11/goutil/shellquote"
	goutilsysopen "github.com/brohd11/goutil/sysopen"

	tea "charm.land/bubbletea/v2"
)

// start runs cmd detached and reports a failure on the status line: an emulator rejecting
// an option exits immediately, and silence would look like success.
func start(cmd *exec.Cmd, what string) tea.Msg {
	if err := cmd.Start(); err != nil {
		return core.SetStatusAndLog("could not open " + what + ": " + err.Error()).Msg
	}
	go cmd.Wait() //nolint:errcheck // reap the child; a terminal that stays open just parks this goroutine
	return nil
}

// Path opens path in the file manager, highlighting a file in its folder when reveal is
// set.
func Path(path string, reveal bool) core.Action {
	if _, err := os.Stat(path); err != nil {
		return core.SetStatusAndLog("path not found: " + path)
	}
	return core.Seq(
		core.SetStatus("opening "+path),
		core.Async(func() tea.Msg {
			if err := goutilsysopen.OpenPath(path, reveal); err != nil {
				return core.SetStatusAndLog("could not " + err.Error()).Msg
			}
			return nil
		}),
	)
}

// URL opens target in the default web browser. target is used as-is — any host/scheme
// normalization is the caller's job (this package names no domain type).
func URL(target string) core.Action {
	if target == "" {
		return core.SetStatusAndLog("no url")
	}
	return core.Seq(
		core.SetStatus("opening "+target),
		core.Async(func() tea.Msg {
			if err := goutilsysopen.OpenURL(target); err != nil {
				return core.SetStatusAndLog("could not " + err.Error()).Msg
			}
			return nil
		}),
	)
}

// Terminal opens a terminal at dir, running command in it when given. It uses the system
// terminal on darwin and windows, and the first known emulator on PATH on Linux (a "not
// found" status when there is none).
func Terminal(dir string, command ...string) core.Action {
	if _, err := os.Stat(dir); err != nil {
		return core.SetStatusAndLog("path not found: " + dir)
	}
	cmd := terminalCmd(dir, command)
	if cmd == nil {
		return core.SetStatusAndLog("no terminal emulator found")
	}
	return core.Seq(
		core.SetStatus("opening terminal at "+dir),
		core.Async(func() tea.Msg {
			return start(cmd, "terminal at "+dir)
		}),
	)
}

// TerminalInline hands this process's terminal to a shell at dir and restores the TUI
// when it exits, so no window is left behind. Interactive zsh, bash and fish shells show
// the app above each prompt; other shells get an entry reminder. With a command it runs
// that directly, without shell integration.
func TerminalInline(dir string, command ...string) core.Action {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return TerminalInlineFor(name, dir, command...)
}

// TerminalInlineFor is TerminalInline naming the app to return to. Names accumulate in
// the child environment across nested shells.
func TerminalInlineFor(appName, dir string, command ...string) core.Action {
	if _, err := os.Stat(dir); err != nil {
		return core.SetStatusAndLog("path not found: " + dir)
	}
	cmd := inlineCmd(command)
	cmd.Dir = dir
	report := func(err error) tea.Msg {
		if err != nil {
			return core.SetStatusAndLog("terminal at " + dir + ": " + err.Error()).Msg
		}
		return core.SetStatus("terminal at " + dir + " closed").Msg
	}
	if len(command) == 0 {
		return core.Async(tea.Exec(&inlineShell{Cmd: cmd, appName: appName}, report))
	}
	return core.Async(tea.ExecProcess(cmd, report))
}

// inlineCmd builds the TerminalInline child: the user's shell, or command run directly.
// The shell gets no -i or -l: it has the real tty and detects that itself, and the flags
// differ across shells.
func inlineCmd(command []string) *exec.Cmd {
	if len(command) > 0 {
		cmd, err := executil.Command(command...)
		if err == nil {
			return cmd
		}
		// Preserve tea.ExecProcess's error-reporting path without changing this
		// internal builder's shape: Run returns Cmd.Err before starting anything.
		failed := exec.Command(command[0], command[1:]...)
		failed.Err = err
		return failed
	}
	return exec.Command(userShell())
}

// userShell is $SHELL, else COMSPEC on Windows, else /bin/sh.
func userShell() string {
	if runtime.GOOS == "windows" {
		if sh := os.Getenv("COMSPEC"); sh != "" {
			return sh
		}
		return "cmd.exe"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// terminalCmd builds the terminal launch for dir, or nil when Linux has no known
// emulator.
func terminalCmd(dir string, command []string) *exec.Cmd {
	cmd := buildTerminalCmd(dir, command)
	if cmd == nil {
		return nil
	}
	// The terminal inherits dir as its cwd, so emulators that ignore the directory option
	// still open in the right place.
	cmd.Dir = dir
	return cmd
}

// buildTerminalCmd picks the per-OS terminal command for dir + command.
func buildTerminalCmd(dir string, command []string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return darwinTerminal(dir, command)
	case "windows":
		return windowsTerminal(dir, command)
	default:
		return probeTerminal(dir, command)
	}
}

// linuxTerminal describes one emulator: its working-directory args ({dir} placeholder;
// nil to rely on cmd.Dir) and the flag that introduces a command ("--", "-e", "-x", or ""
// for positional).
type linuxTerminal struct {
	bin      string
	dirArgs  []string
	execForm string
}

// linuxTerminals is the probe order. x-terminal-emulator is last: its wrapper for
// gnome-terminal drops --working-directory.
var linuxTerminals = []linuxTerminal{
	{"gnome-terminal", []string{"--working-directory={dir}"}, "--"},
	{"ptyxis", []string{"--working-directory={dir}"}, "--"}, // GNOME's current default terminal
	{"kgx", []string{"--working-directory={dir}"}, "--"},    // GNOME Console
	{"konsole", []string{"--workdir", "{dir}"}, "-e"},
	{"kitty", []string{"--directory", "{dir}"}, ""}, // command is positional
	{"alacritty", []string{"--working-directory", "{dir}"}, "-e"},
	{"wezterm", []string{"start", "--cwd", "{dir}"}, "--"},
	{"foot", []string{"--working-directory={dir}"}, ""}, // command is positional
	{"tilix", []string{"-w", "{dir}"}, "-e"},
	{"terminator", []string{"--working-directory={dir}"}, "-x"},
	{"xfce4-terminal", []string{"--working-directory={dir}"}, "-x"},
	{"mate-terminal", []string{"--working-directory={dir}"}, "-x"},
	{"lxterminal", []string{"--working-directory={dir}"}, "-e"},
	{"urxvt", nil, "-e"},
	{"st", nil, "-e"},
	{"xterm", nil, "-e"},
	{"x-terminal-emulator", nil, "-e"},
}

// probeTerminal returns the first emulator on PATH built for dir + command, or nil when
// none is installed.
func probeTerminal(dir string, command []string) *exec.Cmd {
	for _, t := range linuxTerminals {
		if _, err := exec.LookPath(t.bin); err != nil {
			continue
		}
		args := make([]string, 0, len(t.dirArgs)+len(command)+1)
		for _, a := range t.dirArgs {
			args = append(args, strings.ReplaceAll(a, "{dir}", dir))
		}
		if len(command) > 0 {
			if t.execForm != "" {
				args = append(args, t.execForm)
			}
			args = append(args, command...)
		}
		return exec.Command(t.bin, args...)
	}
	return nil
}

// darwinTerminal opens Terminal.app: `open -a Terminal <dir>` for a shell, or osascript
// `do script "cd <dir> && <cmd>"` for a command, since Terminal.app takes no argv.
func darwinTerminal(dir string, command []string) *exec.Cmd {
	if len(command) == 0 {
		return exec.Command("open", "-a", "Terminal", dir)
	}
	script := "cd " + ShellQuote(dir) + " && " + ShellJoin(command)
	return exec.Command("osascript", "-e", `tell application "Terminal" to do script `+appleScriptQuote(script))
}

// windowsTerminalArgs builds the cmd.exe invocation apart from CREATE_NEW_CONSOLE so its
// quoting is testable on any host. cmd.Dir supplies the directory.
func windowsTerminalArgs(command []string) []string {
	args := []string{"cmd.exe", "/d", "/v:off", "/k"}
	if len(command) > 0 {
		line, _ := executil.CmdJoin(command)
		args = append(args, line)
	}
	return args
}

// ShellQuote re-exports goutil/shellquote.Quote for existing callers.
func ShellQuote(s string) string { return shellquote.Quote(s) }

// ShellJoin quotes each argument (ShellQuote) and joins them with spaces into one shell
// command line, so the shell that parses it splits the words back exactly where they started.
func ShellJoin(args []string) string { return shellquote.Join(args) }

// appleScriptQuote wraps s as an AppleScript string literal (double quotes, with `"`
// and `\` backslash-escaped).
func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
