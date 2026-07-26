package sysopen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// stubEmulator writes an executable named bin into a fresh dir and points PATH at it,
// so probeTerminal finds it without a real terminal being involved. The stub records
// its own working directory to cwdFile when run.
func stubEmulator(t *testing.T, bin, cwdFile string) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, bin)
	script := "#!/bin/sh\n"
	if cwdFile != "" {
		script += "pwd > " + cwdFile + "\n"
	}
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return stub
}

func TestProbeTerminalDirOnly(t *testing.T) {
	stub := stubEmulator(t, "konsole", "")
	cmd := probeTerminal("/tmp/work", nil)
	if cmd == nil {
		t.Fatal("expected the stub konsole to be found")
	}
	if cmd.Path != stub {
		t.Fatalf("cmd.Path = %q, want %q", cmd.Path, stub)
	}
	want := []string{"konsole", "--workdir", "/tmp/work"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, want)
	}
}

func TestProbeTerminalCommand(t *testing.T) {
	tests := []struct {
		bin     string
		command []string
		want    []string
	}{
		{"gnome-terminal", []string{"ssh", "host"}, []string{"gnome-terminal", "--working-directory=/tmp/work", "--", "ssh", "host"}},
		{"konsole", []string{"ssh", "host"}, []string{"konsole", "--workdir", "/tmp/work", "-e", "ssh", "host"}},
		{"kitty", []string{"ssh", "host"}, []string{"kitty", "--directory", "/tmp/work", "ssh", "host"}}, // positional
		{"wezterm", []string{"ssh", "host"}, []string{"wezterm", "start", "--cwd", "/tmp/work", "--", "ssh", "host"}},
		{"xterm", []string{"ssh", "host"}, []string{"xterm", "-e", "ssh", "host"}}, // no dir flag
	}
	for _, tc := range tests {
		t.Run(tc.bin, func(t *testing.T) {
			stubEmulator(t, tc.bin, "")
			cmd := probeTerminal("/tmp/work", tc.command)
			if cmd == nil {
				t.Fatalf("expected the stub %s to be found", tc.bin)
			}
			if !reflect.DeepEqual(cmd.Args, tc.want) {
				t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, tc.want)
			}
		})
	}
}

func TestProbeTerminalNone(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: nothing to find
	if cmd := probeTerminal("/tmp/work", nil); cmd != nil {
		t.Fatalf("expected nil with no emulator on PATH, got %v", cmd.Args)
	}
}

// TestProbeTerminalWorkingDir is the regression test for the reported bug: an emulator
// that ignores the working-directory flag must still come up in the target directory,
// because the cwd is set on the command. The stub is a Linux emulator (probeTerminal is
// the linux path, exercised on any host), drops every arg — what x-terminal-emulator's
// gnome-terminal wrapper does — and records where it actually ran.
func TestProbeTerminalWorkingDir(t *testing.T) {
	home := t.TempDir()
	out := filepath.Join(home, "cwd.txt")
	stubEmulator(t, "gnome-terminal", out)

	target := filepath.Join(home, "work", "sub")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := probeTerminal(target, nil)
	if cmd == nil {
		t.Fatal("probeTerminal returned nil")
	}
	cmd.Dir = target // what terminalCmd does before starting the command
	if err := cmd.Run(); err != nil {
		t.Fatalf("stub emulator: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// macOS resolves /var → /private/var, so compare the resolved forms.
	wantDir, _ := filepath.EvalSymlinks(target)
	gotDir, _ := filepath.EvalSymlinks(strings.TrimSpace(string(got)))
	if gotDir != wantDir {
		t.Fatalf("emulator ran in %q, want %q", gotDir, wantDir)
	}
}

func TestDarwinTerminal(t *testing.T) {
	// Dir only: open -a Terminal <dir>.
	cmd := darwinTerminal("/tmp/work", nil)
	want := []string{"open", "-a", "Terminal", "/tmp/work"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("dir-only cmd.Args = %#v, want %#v", cmd.Args, want)
	}

	// With a command: osascript do-script carrying `cd <dir> && <cmd>`.
	cmd = darwinTerminal("/tmp/my work", []string{"ssh", "user@host"})
	if len(cmd.Args) != 3 || cmd.Args[0] != "osascript" || cmd.Args[1] != "-e" {
		t.Fatalf("unexpected osascript invocation: %#v", cmd.Args)
	}
	script := cmd.Args[2]
	for _, sub := range []string{
		`tell application "Terminal" to do script`,
		`cd '/tmp/my work'`,
		`'ssh' 'user@host'`,
	} {
		if !strings.Contains(script, sub) {
			t.Fatalf("osascript missing %q in:\n%s", sub, script)
		}
	}
}

func TestWindowsTerminal(t *testing.T) {
	cmd := windowsTerminal(`C:\work`, nil)
	want := []string{"cmd", "/c", "start", "cmd", "/k", `cd /d C:\work`}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("dir-only cmd.Args = %#v, want %#v", cmd.Args, want)
	}

	cmd = windowsTerminal(`C:\work`, []string{"ssh", "host"})
	want = []string{"cmd", "/c", "start", "cmd", "/k", `cd /d C:\work && ssh host`}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("command cmd.Args = %#v, want %#v", cmd.Args, want)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"plain":      "'plain'",
		"with space": "'with space'",
		"it's mine":  `'it'\''s mine'`,
		"/tmp/a b/c": "'/tmp/a b/c'",
	}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
