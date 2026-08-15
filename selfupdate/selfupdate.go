// Package selfupdate bridges goutil's self-update library into the shared
// self-update TUI flow in bubblestack/components. It is the one bubblestack
// package allowed to import goutil: components stays app-agnostic by design
// (apps inject their release check/install as components.SelfUpdateHooks), and
// before this package existed every goutil-based app copy-pasted the same hook
// adapter and struct conversion. Apps should import this package instead of
// wiring goutil themselves.
package selfupdate

import (
	"context"

	"github.com/brohd11/goutil/selfupdate"

	"github.com/brohd11/bubblestack/components"
)

// Hooks builds the shared self-update flow's hook set for an app: the app name,
// the GitHub repo ("owner/name") to check, and the running version. Check calls
// goutil's selfupdate.Check; Apply installs into the running binary's directory
// via selfupdate.BinDir. The conversion between goutil's selfupdate.Info and the
// flow's app-agnostic components.SelfUpdateInfo is a direct one — the structs
// are field-identical by design, and the drift test in this package pins that.
func Hooks(appName, repo, version string) components.SelfUpdateHooks {
	return components.SelfUpdateHooks{
		AppName: appName,
		Check: func(ctx context.Context) (components.SelfUpdateInfo, error) {
			info, err := selfupdate.Check(ctx, repo, version)
			return components.SelfUpdateInfo(info), err
		},
		Apply: func(ctx context.Context, info components.SelfUpdateInfo, report func(string, ...any)) error {
			binDir, err := selfupdate.BinDir()
			if err != nil {
				return err
			}
			return selfupdate.Apply(ctx, repo, selfupdate.Info(info), binDir, report)
		},
	}
}
