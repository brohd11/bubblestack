// Package selfupdate builds components.SelfUpdateHooks from goutil's self-update library.
package selfupdate

import (
	"context"

	"github.com/brohd11/goutil/selfupdate"

	"github.com/brohd11/bubblestack/components"
)

// Hooks builds an app's self-update hooks: Check calls goutil's selfupdate.Check and
// Apply installs into the running binary's directory. The Info structs are
// field-identical (pinned by a test here), so they convert directly.
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
