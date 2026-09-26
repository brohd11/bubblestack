//go:build unix

package sysopen

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// This subprocess helper opens the next shell in the nesting test using the real
// inherited environment. Input files keep shell read-ahead from consuming commands
// intended for an inner shell.
func TestNestedShellProcess(t *testing.T) {
	app := os.Getenv("BUBBLESTACK_TEST_APP")
	if app == "" {
		return
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		tty.Close()
		t.Fatal("nested shell helper inherited a controlling terminal")
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
	// These shells read command files, not a terminal. Keep every descendant in
	// the helper's process group so cancellation can terminate the whole tree.
	cmd.Args = append(cmd.Args, "-i", "+m", "+Z")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func nestedShellCommand(t *testing.T, ctx context.Context) *exec.Cmd {
	t.Helper()
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
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNestedShellProcess$")
	cmd.Env = []string{
		"HOME=" + home, "TMPDIR=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "TERM=xterm",
		"BUBBLESTACK_TEST_APP=gofer", "BUBBLESTACK_TEST_SHELL=" + shell,
		"BUBBLESTACK_TEST_INPUT=" + filepath.Join(home, "outer"),
	}
	cmd.Dir = home
	return cmd
}

// A separate session prevents /dev/tty from reaching the user's terminal. zsh's
// job control is disabled above so nested helpers stay in this process group.
func nestedShellOutput(t *testing.T, cmd *exec.Cmd) ([]byte, error) {
	t.Helper()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = time.Second
	var killOnce sync.Once
	var killErr error
	killGroup := func() error {
		// Cancel and deferred cleanup share one attempt. Signaling again after
		// termination can hit unreaped processes (or a reused process group).
		killOnce.Do(func() {
			if cmd.Process == nil {
				return
			}
			killErr = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if errors.Is(killErr, syscall.ESRCH) {
				killErr = os.ErrProcessDone
			}
		})
		return killErr
	}
	cmd.Cancel = killGroup
	defer func() {
		if err := killGroup(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("clean up nested shells: %v", err)
		}
	}()
	return cmd.CombinedOutput()
}

func TestNestedShellUnwinds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := nestedShellOutput(t, nestedShellCommand(t, ctx))
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

func TestNestedShellTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := nestedShellCommand(t, ctx)
	// Block the innermost shell while it still holds the captured output pipe.
	// Killing only the outer helper would leave this process alive and Wait stuck.
	writeShellFile(t, cmd.Dir, "inner", "printf 'INNER_BLOCKED\\n'\nexec sleep 60\n")
	start := time.Now()
	out, err := nestedShellOutput(t, cmd)
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected deadline failure, got %v (context: %v)\n%s", err, ctx.Err(), out)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Errorf("timeout took %s, want at most 8s", elapsed)
	}
	if !strings.Contains(string(out), "INNER_BLOCKED") {
		t.Fatalf("innermost shell did not start before timeout:\n%s", out)
	}
	// An orphan may briefly remain as a zombie until the OS reaps it. It has
	// already exited and closed its descriptors; only live group members leak.
	processes, err := exec.Command("ps", "-eo", "pgid=,stat=").Output()
	if err != nil {
		t.Fatal(err)
	}
	group := strconv.Itoa(cmd.Process.Pid)
	for _, line := range strings.Split(string(processes), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == group && !strings.HasPrefix(fields[1], "Z") {
			t.Errorf("nested process group still running after timeout: %s", line)
		}
	}
}
