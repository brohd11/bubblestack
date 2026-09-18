package bubblestack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/config"
	"github.com/brohd11/bubblestack/core"
)

type densityApp struct{ compact bool }

func (a *densityApp) ListDensity() *bool { return &a.compact }

type nilDensityApp struct{}

func (nilDensityApp) ListDensity() *bool { return nil }

func TestInitListDensity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, compact := range []bool{true, false} {
		if err := config.SaveListDensity(compact); err != nil {
			t.Fatal(err)
		}
		app := &densityApp{compact: !compact}
		sh := core.NewShared(app)
		if sh.SaveListDensity != nil || app.compact == compact {
			t.Fatal("direct hosts must not load or save preferences")
		}
		initListDensity(sh)
		if sh.SaveListDensity == nil {
			t.Fatal("startup must install persistence")
		}
		core.NewRouter(sh, []core.TabEntry{{Title: "Root", New: func(sh *core.Shared) core.Screen {
			if app.compact != compact {
				t.Fatal("preference must be restored before root construction")
			}
			return components.NewRootList(nil, components.RootListOpts{})
		}}})
		if err := sh.SaveListDensity(!compact); err != nil {
			t.Fatal(err)
		}
		next := &densityApp{compact: compact}
		initListDensity(core.NewShared(next))
		if next.compact == compact || app.compact != compact {
			t.Fatal("next startup must inherit the save without changing running apps")
		}
	}
	for _, app := range []any{nil, struct{}{}, nilDensityApp{}} {
		sh := core.NewShared(app)
		initListDensity(sh)
		if sh.SaveListDensity != nil {
			t.Fatal("non-participating apps must not persist density")
		}
	}
}

func TestInitListDensityRetainsDefault(t *testing.T) {
	for _, content := range []string{"", "theme: mono\n", "list_density: invalid\n", "list_density: [\n"} {
		t.Run(content, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if content != "" {
				dir := filepath.Join(home, ".bubblestack")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, compact := range []bool{true, false} {
				app := &densityApp{compact: compact}
				initListDensity(core.NewShared(app))
				if app.compact != compact {
					t.Fatal("unavailable preference must retain the app default")
				}
			}
		})
	}
}
