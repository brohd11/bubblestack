package sysopen

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

const shellContextEnv = "BUBBLESTACK_SHELL_CONTEXT"
const shellHintEnv = "BUBBLESTACK_SHELL_HINT"

// inlineShell prepares the integration only once Bubble Tea has released the tty.
// Keeping all resources inside Run also avoids leaks if releasing the tty fails.
type inlineShell struct {
	*exec.Cmd
	appName string
}

func (s *inlineShell) SetStdin(r io.Reader)  { s.Stdin = r }
func (s *inlineShell) SetStdout(w io.Writer) { s.Stdout = w }
func (s *inlineShell) SetStderr(w io.Writer) { s.Stderr = w }

func (s *inlineShell) Run() error {
	env, hint := shellEnvironment(s.Environ(), s.appName)
	s.Env = env
	cleanup, supported, err := prepareShell(s.Cmd)
	defer cleanup()
	if err != nil {
		fmt.Fprintln(s.Stderr, "App prompt reminder unavailable; showing it once on entry.")
	}
	if !supported || err != nil {
		fmt.Fprintln(s.Stdout, hint)
	}
	return s.Cmd.Run()
}

func shellEnvironment(env []string, appName string) ([]string, string) {
	var chain []string
	if err := json.Unmarshal([]byte(envValue(env, shellContextEnv)), &chain); err != nil {
		chain = nil
	}
	for i := range chain {
		chain[i] = shellLabel(chain[i])
	}
	name := shellLabel(appName)
	chain = append(chain, name)
	encoded, _ := json.Marshal(chain)
	hint := "[" + strings.Join(chain, " → ") + "] exit returns to " + name
	env = withEnv(env, shellContextEnv, string(encoded))
	return withEnv(env, shellHintEnv, hint), hint
}

func shellLabel(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, s))
	if s == "" {
		return "app"
	}
	return s
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(env[i], key+"="); ok {
			return value
		}
	}
	return ""
}

func withEnv(env []string, key, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

// prepareShell changes argv/environment only after all startup files are written.
// An error therefore leaves a usable plain shell for the entry-message fallback.
func prepareShell(cmd *exec.Cmd) (cleanup func(), supported bool, err error) {
	cleanup = func() {}
	shell := strings.TrimSuffix(filepath.Base(cmd.Path), ".exe")
	if shell == "fish" {
		cmd.Args = append(cmd.Args, "--init-command", fishPromptHook)
		return cleanup, true, nil
	}
	if shell != "zsh" && shell != "bash" {
		return cleanup, false, nil
	}
	dir, err := os.MkdirTemp("", "bubblestack-shell-")
	if err != nil {
		return cleanup, true, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	write := func(name, body string) error {
		return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
	}
	if shell == "bash" {
		if err := write("bashrc", bashPromptHook); err != nil {
			return cleanup, true, err
		}
		cmd.Args = append(cmd.Args, "--rcfile", filepath.Join(dir, "bashrc"))
		return cleanup, true, nil
	}

	// .zshenv may itself change ZDOTDIR. Let it run with the original value,
	// remember its resulting value, then route .zshrc through our temporary file.
	// Restore that value before sourcing the user's .zshrc and before any children.
	restore := "unset ZDOTDIR\n"
	for _, entry := range cmd.Environ() {
		if value, ok := strings.CutPrefix(entry, "ZDOTDIR="); ok {
			restore = "export ZDOTDIR=" + ShellQuote(value) + "\n"
		}
	}
	zshenv := restore + `if [[ -o rcs && -r ${ZDOTDIR-$HOME}/.zshenv ]]; then
  source "${ZDOTDIR-$HOME}/.zshenv"
fi
if [[ ! -o rcs ]]; then
  # Respect startup opt-out and leave the real ZDOTDIR available to children.
  [[ -o interactive ]] && builtin printf '%s\n' "$BUBBLESTACK_SHELL_HINT"
  return
fi
typeset -g _bubblestack_zdotdir_set=${+ZDOTDIR}
typeset -g _bubblestack_zdotdir=${ZDOTDIR-}
` + "export ZDOTDIR=" + ShellQuote(dir) + "\n"
	if err := write(".zshenv", zshenv); err != nil {
		return cleanup, true, err
	}
	if err := write(".zshrc", zshPromptHook); err != nil {
		return cleanup, true, err
	}
	cmd.Env = withEnv(cmd.Environ(), "ZDOTDIR", dir)
	return cleanup, true, nil
}

const zshPromptHook = `if (( _bubblestack_zdotdir_set )); then
  export ZDOTDIR=$_bubblestack_zdotdir
else
  unset ZDOTDIR
fi
unset _bubblestack_zdotdir_set _bubblestack_zdotdir
if [[ -o rcs && -r ${ZDOTDIR-$HOME}/.zshrc ]]; then
  source "${ZDOTDIR-$HOME}/.zshrc"
fi
function _bubblestack_prompt_hint {
  local ret=$?
  builtin printf '%s\n' "$BUBBLESTACK_SHELL_HINT"
  return "$ret"
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd _bubblestack_prompt_hint
`

const bashPromptHook = `if [ -r "$HOME/.bashrc" ]; then
  source "$HOME/.bashrc"
fi
_bubblestack_prompt_hint() {
  local ret=$?
  builtin printf '%s\n' "$BUBBLESTACK_SHELL_HINT"
  return "$ret"
}
# Preserve the user's command (and its input status), including array form on
# newer bash. Scalar form also works on macOS's bash 3.2.
case "$(declare -p PROMPT_COMMAND 2>/dev/null)" in
  'declare -a '*) PROMPT_COMMAND=(_bubblestack_prompt_hint "${PROMPT_COMMAND[@]}") ;;
  *) PROMPT_COMMAND="_bubblestack_prompt_hint${PROMPT_COMMAND:+; $PROMPT_COMMAND}" ;;
esac
`

const fishPromptHook = `function _bubblestack_prompt_hint --on-event fish_prompt
  set -l ret $status
  printf '%s\n' "$BUBBLESTACK_SHELL_HINT"
  return $ret
end`
