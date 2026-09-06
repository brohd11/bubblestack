# bubblestack

A small, reusable [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework:
a router over a screen stack, plus context-agnostic, closure-configured components. It names
no application domain type — a consumer supplies its context (recovered via `core.App[T]`),
optional header/output/status chrome, a theme, and its tabs, and `bubblestack.Run` wires the rest.

- **`core/`** — `Shared` state, the `Router` and navigation `Action`s (Push/Pop/Replace/…),
  the `Screen` interface and optional capabilities (Receiver/Crumber/Overlayer/…), a theme
  registry, and layout/help/style helpers.
- **`components/`** — reusable screens configured by closures: a self-dispatching list `Item`,
  `PickerScreen`, `DialogScreen`, `MenuScreen` (the floating dropdown/context menu),
  `LoadingScreen`, `TaskScreen`, `FormScreen`, `DocScreen`, and the default
  `LogPane`/`StatusLine`.
- **`sysopen/`** — hand a path/URL/directory to the OS: `Path` (file manager), `URL` (browser),
  `Terminal(dir, command...)` (an emulator at `dir`, optionally running a command) and
  `TerminalInline(dir, command...)` (the same, but borrowing the running program's own tty via
  `tea.ExecProcess` — the TUI suspends and is restored on exit, so no window is spawned).
  Returns a `core.Action`; cross-platform, with the launched terminal always rooted at `dir`.

Used by [gdaddon](https://github.com/brohd11/gdaddon) and
[repoview](https://github.com/brohd11/repoview).

```go
import "github.com/brohd11/bubblestack"
```

`components.NewModularScreen` arranges panels as columns of weighted rows.
For nested arrangements, `components.NewModularLayout` accepts horizontal and
vertical groups. All leaves share one screen's focus, input routing, and lifecycle:

```go
leaf := func(p components.Panel) components.LayoutNode {
    return components.LayoutNode{Slot: &components.Slot{Panel: p}}
}
screen := components.NewModularLayout(components.LayoutNode{
    ID: "workspace", Axis: components.LayoutVertical,
    Children: []components.LayoutNode{
        {ID: "main", Axis: components.LayoutHorizontal, Weight: 3,
            Children: []components.LayoutNode{leaf(files), leaf(editor)}},
        {ID: "tools", Axis: components.LayoutHorizontal,
            Children: []components.LayoutNode{leaf(results)}},
    },
}, components.ModularOpts{Resize: &components.ResizeOpts{}})
```

Here `results` spans the width below `files` and `editor`. Adding another leaf to
`tools.Children` splits that row without changing the upper row. A node's positive
`Size` fixes its width or height along its parent's axis; otherwise `Weight` divides
the remaining space. Groups and leaves fill their allocations, with minimum sizes
derived from `ResizeOpts` and clipping when the terminal cannot fit them.

Stable group IDs identify entries in `ResizeState.Splits`. Keep snapshots through
`ResizeOpts.OnChange` and restore them through `ResizeOpts.State`. Applications own
visibility, toggle shortcuts, and retained state for groups they temporarily remove.
