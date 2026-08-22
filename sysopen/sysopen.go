// Package sysopen hands a path, URL, or directory to the OS: the file manager
// (Path), the default browser (URL), and a terminal emulator (Terminal, or
// TerminalInline for a shell that borrows this process's own tty). It names no
// application domain type and imports only core + stdlib, so any bubblestack consumer
// (gdaddon, repoview, …) can reuse it instead of copying the launcher logic.
//
// The load-bearing detail on Linux is that every launched command gets cmd.Dir set to
// the target directory: emulators disagree on the working-directory option and some
// wrappers (x-terminal-emulator → gnome-terminal.wrapper) silently drop it, so relying
// on the flag alone opens the terminal at the process's own cwd. Setting the cwd makes
// the flag belt-and-suspenders rather than load-bearing.
package sysopen

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// start runs cmd detached and reports the failure on the status line rather than
// swallowing it — a terminal emulator that rejects an option dies immediately, and
// silence there is indistinguishable from a working launch. The returned message is a
// framework control message; the router applies it when the async cmd's result lands.
func start(cmd *exec.Cmd, what string) tea.Msg {
	if err := cmd.Start(); err != nil {
		return core.SetStatusAndLog("could not open " + what + ": " + err.Error()).Msg
	}
	go cmd.Wait() //nolint:errcheck // reap the child; a terminal that stays open just parks this goroutine
	return nil
}

// Path opens path in the OS file manager. When reveal is set (used for a file like a
// manifest), the file is highlighted within its containing folder; otherwise path is
// opened directly as a directory.
func Path(path string, reveal bool) core.Action {
	if _, err := os.Stat(path); err != nil {
		return core.SetStatusAndLog("path not found: " + path)
	}
	return core.Seq(
		core.SetStatus("opening "+path),
		core.Async(func() tea.Msg {
			return start(pathCmd(path, reveal), path)
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
			return start(urlCmd(target), target)
		}),
	)
}

// Terminal opens a terminal at dir. With a command it runs that command in the
// terminal (still rooted at dir); without one it opens an interactive shell there.
// Callers just pass their directory and, optionally, the argv to run.
//
// The chosen emulator: on darwin/windows the system terminal, on linux the first known
// emulator on PATH (see linuxTerminals). Returns a "not found" status when linux has no
// known emulator installed.
//
// NOTE(config): a future bubblestack-owned config (a settable config path supplying a
// user terminal-override template + theme) will plug in here, ahead of auto-detection.
// Until then this is auto-detect only.
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

// TerminalInline hands this process's terminal to a shell at dir: bubbletea releases the
// tty, the child owns it until it exits, and the TUI is restored. The in-process sibling
// of Terminal — no window is created, so the user lands back on the screen they left
// rather than accumulating detached windows for a two-command detour.
//
// With a command it runs that command instead of a shell (still rooted at dir), matching
// Terminal's variadic. Unlike Terminal there is no pre-launch status: the screen is about
// to disappear, so the only line the user reads is the one written when the child exits.
func TerminalInline(dir string, command ...string) core.Action {
	if _, err := os.Stat(dir); err != nil {
		return core.SetStatusAndLog("path not found: " + dir)
	}
	cmd := inlineCmd(command)
	cmd.Dir = dir
	return core.Async(tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return core.SetStatusAndLog("terminal at " + dir + ": " + err.Error()).Msg
		}
		return core.SetStatus("terminal at " + dir + " closed").Msg
	}))
}

// inlineCmd builds the child for TerminalInline: the user's shell when command is empty,
// otherwise command run directly (no shell, so nothing is re-parsed).
//
// The shell gets no -i or -l: ExecProcess hands it the real tty, so it decides for itself
// that it is interactive, and the flags don't mean the same thing across sh/zsh/fish —
// passing them is how a login-only rc file ends up sourced twice or not at all.
func inlineCmd(command []string) *exec.Cmd {
	if len(command) > 0 {
		return exec.Command(command[0], command[1:]...)
	}
	return exec.Command(userShell())
}

// userShell is the interactive shell to hand the terminal to: the user's own, falling
// back to the one every system has. On windows $SHELL is normally unset, so COMSPEC is
// what actually answers there.
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

// terminalCmd builds the terminal launch command for dir (running command in it, if
// non-empty), or nil when no suitable terminal could be found (linux with no known
// emulator on PATH).
func terminalCmd(dir string, command []string) *exec.Cmd {
	cmd := buildTerminalCmd(dir, command)
	if cmd == nil {
		return nil
	}
	// The launched terminal also inherits this as its cwd, which is the load-bearing
	// part on linux: emulators that don't understand the working-directory option (and
	// wrappers like x-terminal-emulator, which silently drop it) then still open in the
	// right place instead of wherever the process happens to be running from.
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

// linuxTerminal describes how to launch one emulator: the working-directory args (with
// a {dir} placeholder; nil when the emulator has no such flag and we rely on cmd.Dir)
// and the exec form that introduces a command to run — "--", "-e", "-x", or "" for the
// emulators that take the command positionally.
type linuxTerminal struct {
	bin      string
	dirArgs  []string
	execForm string
}

// linuxTerminals is the probe order: the emulator each desktop actually ships first,
// then the rest.
//
// x-terminal-emulator is deliberately last. It's the Debian alternatives symlink, and
// its contract only guarantees the xterm options -T and -e; the gnome-terminal wrapper
// behind it drops --working-directory on the floor, which is how a terminal opened at
// the process's own cwd rather than the intended directory. Kept only as a last resort.
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

// darwinTerminal opens Terminal.app. Without a command, `open -a Terminal <dir>` opens a
// shell there. With one, Terminal.app can't be handed argv on the command line, so we
// drive it through osascript: `do script "cd <dir> && <cmd>"` runs the command in a new
// window/tab.
func darwinTerminal(dir string, command []string) *exec.Cmd {
	if len(command) == 0 {
		return exec.Command("open", "-a", "Terminal", dir)
	}
	script := "cd " + ShellQuote(dir) + " && " + ShellJoin(command)
	return exec.Command("osascript", "-e", `tell application "Terminal" to do script `+appleScriptQuote(script))
}

// windowsTerminal opens a cmd window. /k keeps it open after the command exits so the
// user can read the output (matching an interactive shell).
func windowsTerminal(dir string, command []string) *exec.Cmd {
	inner := "cd /d " + dir
	if len(command) > 0 {
		inner += " && " + strings.Join(command, " ")
	}
	return exec.Command("cmd", "/c", "start", "cmd", "/k", inner)
}

func pathCmd(path string, reveal bool) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			return exec.Command("open", "-R", path)
		}
		return exec.Command("open", path)
	case "windows":
		if reveal {
			return exec.Command("explorer", "/select,"+path)
		}
		return exec.Command("explorer", path)
	default:
		if reveal {
			path = filepath.Dir(path)
		}
		return exec.Command("xdg-open", path)
	}
}

func urlCmd(target string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target)
	case "windows":
		return exec.Command("cmd", "/c", "start", "", target)
	default:
		return exec.Command("xdg-open", target)
	}
}

// ShellQuote wraps s in single quotes for a POSIX shell as a single-quoted literal, so
// nothing inside is expanded or word-split; embedded single quotes are closed, escaped, and
// reopened. Used to build the darwin `cd <dir>` fragment, and shared with consumers that
// cross a shell boundary (e.g. go-ssh's remote command lines).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin quotes each argument (ShellQuote) and joins them with spaces into one shell
// command line, so the shell that parses it splits the words back exactly where they started.
func ShellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = ShellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// appleScriptQuote wraps s as an AppleScript string literal (double quotes, with `"`
// and `\` backslash-escaped).
func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
