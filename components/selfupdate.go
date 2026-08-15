package components

import (
	"context"
	"fmt"
	"time"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// This file holds the shared self-update TUI flow, extracted from two near-identical
// copies — gdaddon's internal/tui/tabs/actions/selfupdate.go and repoview's
// internal/app/update.go — that differed only in the app name, the repo they check,
// and the install destination. It stays app-agnostic on purpose: components must not
// depend on the goutil self-update library (a sibling module), so the app injects its
// release check and install operations as SelfUpdateHooks and this file owns everything
// else — the screens, the navigation, the timeout, and the message strings. Apps that
// do use goutil's self-update library should build their hooks via the bridge package
// bubblestack/selfupdate (the one sanctioned exception to that rule) instead of
// wiring goutil themselves.

// SelfUpdateInfo mirrors the outcome of a release check.
type SelfUpdateInfo struct {
	Current   string
	LatestTag string
	Available bool
}

// SelfUpdateHooks wires an app's self-update machinery into the shared flow:
// AppName is used in crumbs/status lines; Check fetches the latest release info
// (off the UI thread, ctx carries the timeout); Apply downloads+installs info.LatestTag.
type SelfUpdateHooks struct {
	AppName string
	Check   func(ctx context.Context) (SelfUpdateInfo, error)
	Apply   func(ctx context.Context, info SelfUpdateInfo, report func(string, ...any)) error
}

// selfUpdateCheckTimeout caps the release-check fetch so a slow or unreachable host can hang
// neither the loading screen behind the Actions flow nor the startup check.
const selfUpdateCheckTimeout = 30 * time.Second

// selfUpdateInfoMsg carries the self-update check result from the background fetch to
// the loading screen's result handler.
type selfUpdateInfoMsg struct {
	info SelfUpdateInfo
	err  error
}

// NewSelfUpdateLoading is the entry point of the Actions ▸ Update <app> flow:
// loading → confirm → task. It runs hooks.Check off the UI thread; when an update
// exists it opens the confirm, otherwise it reports "up to date" and pops.
func NewSelfUpdateLoading(hooks SelfUpdateHooks) *LoadingScreen {
	cmd := func(parent context.Context) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(parent, selfUpdateCheckTimeout)
			defer cancel()
			info, err := hooks.Check(ctx)
			return selfUpdateInfoMsg{info: info, err: err}
		}
	}
	onResult := func(sh *core.Shared, msg tea.Msg) core.Action {
		m, ok := msg.(selfUpdateInfoMsg)
		if !ok {
			return core.Action{}
		}
		if m.err != nil {
			return core.Seq(core.SetStatusAndLog("update check failed: "+m.err.Error()), core.Pop())
		}
		if !m.info.Available {
			return core.Seq(core.SetStatus(hooks.AppName+" is up to date"), core.Pop())
		}
		return core.Replace(newSelfUpdateConfirm(hooks, m.info))
	}
	return NewLoadingScreen("Update "+hooks.AppName, "checking for "+hooks.AppName+" update…", cmd, onResult)
}

// newSelfUpdateConfirm shows the pending update ("current → latest") and, on confirm,
// runs the download+install task.
func newSelfUpdateConfirm(hooks SelfUpdateHooks, info SelfUpdateInfo) *DialogScreen {
	return CreateConfirmScreen(ConfirmSimple{
		Crumb: "Update " + hooks.AppName,
		Text:  fmt.Sprintf("Update %s %s → %s?", hooks.AppName, info.Current, info.LatestTag),
		OnYes: core.Replace(newSelfUpdateTask(hooks, info)),
	})
}

// newSelfUpdateTask downloads and installs the new binary over the running one, then pops
// back to the Actions root. The running process keeps the old code in memory, so it reports
// that a relaunch picks up the new binary.
func newSelfUpdateTask(hooks SelfUpdateHooks, info SelfUpdateInfo) *TaskScreen {
	run := func(ctx context.Context, sh *core.Shared, report func(string, ...any), done chan<- core.TaskEvent) {
		done <- core.TaskEvent{Done: true, Err: hooks.Apply(ctx, info, report)}
	}
	onDone := func(sh *core.Shared, ev core.TaskEvent) core.Action {
		if ev.Err != nil {
			return core.Seq(
				core.SetStatusAndLog("update failed: "+ev.Err.Error(), true),
				core.Pop(),
			)
		}
		return core.Seq(
			core.SetStatusAndLog(fmt.Sprintf("updated to %s — relaunch %s to use it", info.LatestTag, hooks.AppName), true),
			core.Pop(),
		)
	}
	return NewTask("updating "+hooks.AppName+"…", run, onDone)
}

// SelfUpdateCheckCmd is the app-level startup command (wired onto bubblestack Config.Init):
// it runs hooks.Check off the UI thread and, only when an update is available, writes an
// "update available" line to the shared status line and log. Anything else (up to date,
// dev build, fetch error) is silent. The returned Action rides back on the cmd's tea.Msg
// and is applied by the router.
func SelfUpdateCheckCmd(hooks SelfUpdateHooks) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), selfUpdateCheckTimeout)
		defer cancel()
		info, err := hooks.Check(ctx)
		if err != nil || !info.Available {
			return nil
		}
		return core.SetStatusAndLog(
			fmt.Sprintf("%s update available: %s → %s · Actions ▸ Update %s", hooks.AppName, info.Current, info.LatestTag, hooks.AppName),
			true,
		)
	}
}
