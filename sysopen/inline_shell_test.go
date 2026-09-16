package sysopen

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellContextNesting(t *testing.T) {
	base := []string{"PATH=/bin", shellContextEnv + "=not json"}
	env := base
	for i, name := range []string{"gofer", "repoview", "gofer"} {
		parent := append([]string(nil), env...)
		child, hint := shellEnvironment(env, name)
		if !reflect.DeepEqual(env, parent) {
			t.Fatal("modified the parent environment")
		}
		var chain []string
		if err := json.Unmarshal([]byte(envValue(child, shellContextEnv)), &chain); err != nil {
			t.Fatal(err)
		}
		if len(chain) != i+1 || chain[i] != name {
			t.Fatalf("chain = %v", chain)
		}
		if !strings.HasSuffix(hint, "exit returns to "+name) {
			t.Fatalf("hint = %q", hint)
		}
		env = child
	}
	if got := envValue(env, shellHintEnv); got != "[gofer → repoview → gofer] exit returns to gofer" {
		t.Fatalf("hint = %q", got)
	}
}

func TestShellLabelsAreData(t *testing.T) {
	name := "a'\"$()`%s\n\r\x1b\u202e\u2028"
	_, hint := shellEnvironment([]string{shellContextEnv + `=["outer\u001b\n"]`}, name)
	if want := "[outer → a'\"$()`%s] exit returns to a'\"$()`%s"; hint != want {
		t.Fatalf("hint = %q, want %q", hint, want)
	}
	_, hint = shellEnvironment(nil, "\n\x1b")
	if hint != "[app] exit returns to app" {
		t.Fatalf("empty label hint = %q", hint)
	}
}

func TestShellMalformedContext(t *testing.T) {
	for _, value := range []string{"", "broken", `{"app":"gofer"}`, `["gofer",1]`, "null"} {
		env, hint := shellEnvironment([]string{shellContextEnv + "=" + value}, "repoview")
		if hint != "[repoview] exit returns to repoview" || envValue(env, shellContextEnv) != `["repoview"]` {
			t.Errorf("context %q: %q", value, hint)
		}
	}
}

func shellForTest(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix interactive shell integration")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return path
}

func writeShellFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fish does not draw prompts on pipes, even with -i. The OS's script utility
// supplies a real tty without adding a PTY library to the framework's dependencies.
func interactiveOutput(t *testing.T, cmd *exec.Cmd) ([]byte, error) {
	t.Helper()
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("interactive shell tests require the script utility")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var tty *exec.Cmd
	if runtime.GOOS == "darwin" {
		tty = exec.CommandContext(ctx, script, append([]string{"-q", "/dev/null"}, cmd.Args...)...)
	} else {
		tty = exec.CommandContext(ctx, script, "-q", "-e", "-c", ShellJoin(cmd.Args), "/dev/null")
	}
	tty.Env, tty.Dir = cmd.Env, cmd.Dir
	input, err := tty.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	out := &promptOutput{ready: make(chan struct{})}
	tty.Stdout, tty.Stderr = out, out
	if err := tty.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- tty.Wait() }()
	select {
	case <-out.ready:
		// Wait for initialization before feeding input, and keep stdin open until
		// exit. BSD script otherwise sends EOF before the buffered commands.
		_, _ = io.Copy(input, cmd.Stdin)
		err = <-done
	case err = <-done:
	}
	return bytes.ReplaceAll(out.buf.Bytes(), []byte("\r"), nil), err
}

type promptOutput struct {
	buf   bytes.Buffer
	ready chan struct{}
	seen  bool
}

func (p *promptOutput) Write(b []byte) (int, error) {
	n, err := p.buf.Write(b)
	if !p.seen && bytes.Contains(p.buf.Bytes(), []byte("exit returns to")) {
		p.seen = true
		close(p.ready)
	}
	return n, err
}

// Exercise real startup files, prompt hooks, commands and exit statuses in a tty.
func TestInteractiveShellReminder(t *testing.T) {
	for _, name := range []string{"zsh", "bash", "fish"} {
		t.Run(name, func(t *testing.T) {
			shell := shellForTest(t, name)
			home := t.TempDir()
			config := filepath.Join(home, ".config", "fish")
			if err := os.MkdirAll(config, 0o700); err != nil {
				t.Fatal(err)
			}
			writeShellFile(t, home, ".zshenv", "export ENV_LOADED=$(( ${ENV_LOADED:-0} + 1 ))\n")
			writeShellFile(t, home, ".zshrc", `export RC_LOADED=$(( ${RC_LOADED:-0} + 1 ))
HISTFILE=$HOME/history
SAVEHIST=100
HISTSIZE=100
PROMPT='USER_PROMPT:%? > '
precmd() { local ret=$?; printf 'USER_HOOK:%s\n' "$ret"; return "$ret"; }
alias sample_alias='printf "ALIAS_OK\n"'
`)
			writeShellFile(t, home, ".bashrc", `export RC_LOADED=$(( ${RC_LOADED:-0} + 1 ))
HISTFILE=$HOME/history
PS1='USER_PROMPT:$? > '
PROMPT_COMMAND='ret=$?; printf "USER_HOOK:%s\n" "$ret"; (exit "$ret")'
alias sample_alias='printf "ALIAS_OK\n"'
`)
			writeShellFile(t, config, "config.fish", `set -gx RC_LOADED 1
function fish_prompt
  printf 'USER_PROMPT:%s > ' $status
end
alias sample_alias 'printf "ALIAS_OK\n"'
`)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, shell)
			cmd.Dir = home
			// Do not inherit the user's startup directories, hooks or app context.
			cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm", "XDG_CONFIG_HOME=" + filepath.Dir(config)}
			cmd.Env, _ = shellEnvironment(cmd.Env, "gofer")
			cleanup, supported, err := prepareShell(cmd)
			defer cleanup()
			if err != nil || !supported {
				t.Fatalf("prepare: supported=%v err=%v", supported, err)
			}
			cmd.Args = append(cmd.Args, "-i")
			input := "false\nsample_alias\nprintf 'CONFIG:%s\\n' \"$RC_LOADED\"\npwd\nclear\nprintf 'AFTER_CLEAR\\n'\nexit\n"
			if name == "zsh" {
				input = "printf 'ENV:%s ZDOT:%s\\n' \"$ENV_LOADED\" \"${ZDOTDIR-unset}\"\n" + input
			}
			cmd.Stdin = strings.NewReader(input)
			out, err := interactiveOutput(t, cmd)
			if err != nil {
				t.Fatalf("shell: %v\n%s", err, out)
			}
			text := string(out)
			for _, want := range []string{"USER_PROMPT:1 >", "CONFIG:1", "ALIAS_OK", "AFTER_CLEAR", home} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q:\n%s", want, text)
				}
			}
			if count := strings.Count(text, "[gofer] exit returns to gofer"); count < 6 {
				t.Errorf("only %d reminders:\n%s", count, text)
			}
			if name == "zsh" && !strings.Contains(text, "ENV:1 ZDOT:unset") {
				t.Errorf("startup environment changed:\n%s", text)
			}
			if name != "fish" {
				history, err := os.ReadFile(filepath.Join(home, "history"))
				if err != nil || !bytes.Contains(history, []byte("sample_alias")) {
					t.Errorf("history was not saved: %v, %q", err, history)
				}
			}
		})
	}
}

func TestZshCustomStartupDirectory(t *testing.T) {
	shell := shellForTest(t, "zsh")
	home, initial, redirected := t.TempDir(), t.TempDir(), t.TempDir()
	writeShellFile(t, initial, ".zshenv", "export ZDOTDIR="+ShellQuote(redirected)+"\nexport ENV_LOADED=yes\n")
	writeShellFile(t, redirected, ".zshrc", "PROMPT='CUSTOM > '\nexport RC_LOADED=yes\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "ZDOTDIR=" + initial, "TERM=xterm"}
	cmd.Env, _ = shellEnvironment(cmd.Env, "repoview")
	cleanup, _, err := prepareShell(cmd)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Args = append(cmd.Args, "-i")
	cmd.Stdin = strings.NewReader("printf 'CONFIG:%s:%s:%s\\n' \"$ENV_LOADED\" \"$RC_LOADED\" \"$ZDOTDIR\"\nexit\n")
	out, err := interactiveOutput(t, cmd)
	if err != nil || !strings.Contains(string(out), "CONFIG:yes:yes:"+redirected) || !strings.Contains(string(out), "[repoview] exit returns to repoview") {
		t.Fatalf("custom startup: %v\n%s", err, out)
	}
}

func TestBashArrayPromptCommand(t *testing.T) {
	shell := shellForTest(t, "bash")
	if err := exec.Command(shell, "-c", "(( BASH_VERSINFO[0] >= 5 ))").Run(); err != nil {
		t.Skip("array PROMPT_COMMAND requires modern bash")
	}
	home := t.TempDir()
	writeShellFile(t, home, ".bashrc", `PS1='PROMPT > '
PROMPT_COMMAND=('printf "FIRST:%s\n" "$?"' 'printf "SECOND\n"')
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm"}
	cmd.Env, _ = shellEnvironment(cmd.Env, "gofer")
	cleanup, _, err := prepareShell(cmd)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Args = append(cmd.Args, "-i")
	cmd.Stdin = strings.NewReader("false\nexit\n")
	out, err := interactiveOutput(t, cmd)
	if err != nil || !strings.Contains(string(out), "[gofer] exit returns to gofer\nFIRST:1\nSECOND\n") {
		t.Fatalf("array prompt hooks: %v\n%s", err, out)
	}
}

// This subprocess helper opens the next shell in the nesting test using the real
// inherited environment. Input files keep shell read-ahead from consuming commands
// intended for an inner shell.
func TestNestedShellProcess(t *testing.T) {
	app := os.Getenv("BUBBLESTACK_TEST_APP")
	if app == "" {
		return
	}
	input, err := os.Open(os.Getenv("BUBBLESTACK_TEST_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	cmd := exec.Command(os.Getenv("BUBBLESTACK_TEST_SHELL"))
	cmd.Env, _ = shellEnvironment(cmd.Environ(), app)
	cleanup, _, err := prepareShell(cmd)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Args = append(cmd.Args, "-i")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestNestedShellUnwinds(t *testing.T) {
	shell := shellForTest(t, "zsh")
	home := t.TempDir()
	writeShellFile(t, home, ".zshrc", "PROMPT='READY > '\n")
	childCommand := func(app, file string) string {
		return "env BUBBLESTACK_TEST_APP=" + ShellQuote(app) + " BUBBLESTACK_TEST_INPUT=" +
			ShellQuote(filepath.Join(home, file)) + " " + ShellQuote(os.Args[0]) + " -test.run=^TestNestedShellProcess$\n"
	}
	writeShellFile(t, home, "inner", "printf 'INNER\\n'\nexit\n")
	writeShellFile(t, home, "middle", childCommand("gofer", "inner")+"printf 'MIDDLE:%s\\n' \"$BUBBLESTACK_SHELL_HINT\"\nexit\n")
	writeShellFile(t, home, "outer", childCommand("repoview", "middle")+"printf 'OUTER:%s\\n' \"$BUBBLESTACK_SHELL_HINT\"\nexit\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNestedShellProcess$")
	cmd.Env = []string{
		"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm",
		"BUBBLESTACK_TEST_APP=gofer", "BUBBLESTACK_TEST_SHELL=" + shell,
		"BUBBLESTACK_TEST_INPUT=" + filepath.Join(home, "outer"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nested shells: %v\n%s", err, out)
	}
	for _, want := range []string{
		"[gofer → repoview → gofer] exit returns to gofer",
		"MIDDLE:[gofer → repoview] exit returns to repoview",
		"OUTER:[gofer] exit returns to gofer",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestReminderDoesNotEvaluateAppName(t *testing.T) {
	for _, name := range []string{"zsh", "bash", "fish"} {
		t.Run(name, func(t *testing.T) {
			shell := shellForTest(t, name)
			home := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, shell)
			cmd.Dir = home
			cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm"}
			label := "$(touch unexpected)`touch unexpected`'%s"
			cmd.Env, _ = shellEnvironment(cmd.Env, label)
			cleanup, _, err := prepareShell(cmd)
			defer cleanup()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Args = append(cmd.Args, "-i")
			cmd.Stdin = strings.NewReader("exit\n")
			out, err := interactiveOutput(t, cmd)
			if err != nil || !strings.Contains(string(out), "["+label+"] exit returns to "+label) {
				t.Fatalf("literal label: %v\n%s", err, out)
			}
			if _, err := os.Stat(filepath.Join(home, "unexpected")); !os.IsNotExist(err) {
				t.Fatal("app name was evaluated as shell code")
			}
		})
	}
}

func TestShellSetupFailureAndUnknownShell(t *testing.T) {
	shell := shellForTest(t, "bash")
	for _, failSetup := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown shell", true: "setup failure"}[failSetup], func(t *testing.T) {
			if !failSetup {
				shell = shellForTest(t, "sh")
			} else {
				shell = shellForTest(t, "bash")
				t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
			}
			cmd := exec.Command(shell, "-c", "printf 'SHELL_RAN\\n'")
			var stdout, stderr bytes.Buffer
			s := &inlineShell{Cmd: cmd, appName: "gofer"}
			s.SetStdout(&stdout)
			s.SetStderr(&stderr)
			if err := s.Run(); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "[gofer] exit returns to gofer\nSHELL_RAN\n" {
				t.Fatalf("output = %q", stdout.String())
			}
			if failSetup && !strings.Contains(stderr.String(), "unavailable") {
				t.Fatalf("missing fallback explanation: %s", &stderr)
			}
		})
	}
}

func TestInlineShellCleanupOnLaunchFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix temporary directory")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	cmd := exec.Command(filepath.Join(t.TempDir(), "zsh"))
	var out bytes.Buffer
	s := &inlineShell{Cmd: cmd, appName: "gofer"}
	s.SetStdout(&out)
	s.SetStderr(&out)
	if err := s.Run(); err == nil {
		t.Fatal("expected launch failure")
	}
	files, err := os.ReadDir(tmp)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files leaked: %v, %v", files, err)
	}
}
