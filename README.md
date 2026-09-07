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

For a non-resizable strip, set a node's positive `Size` and `FixedSize: true`.
This also overrides the ordinary pane minimum along that axis; a tab bar can
occupy exactly one row. Locked nodes retain their declared size when restoring
a resize snapshot and have no adjacent resize handles.

`components.NewTabBar` creates a reusable single-row tab panel. Supply
`[]components.TabItem` with stable `ID`, `Label`, and optional `Marker` values via
`SetItems`, and select the current ID with `SetActive`. It handles overflow,
cell-width truncation, and keeping the active tab visible on selection or resize.
Call `Click(x, y)` with local coordinates: a nonempty returned ID requests
activation; arrow clicks scroll without selecting. The bar does not take keyboard
focus, so the host owns shortcuts and routes its mouse clicks explicitly.

### FilePanel colors

`FilePanelOpts.Colors` accepts `FileColorsNone` (the zero value), `FileColorsDirs`,
or `FileColorsAll`. Dirs colors navigable directories, including directory symlinks
and `..`; All keeps the file-type palette. Existing `Colors: true` callers should use
`Colors: components.FileColorsAll`; `false` becomes `FileColorsNone` or omission.

An optional `TitleColor func(FileEntry) color.Color` overrides an individual row's
foreground. Return nil to use the mode's default. The callback runs during rendering,
so read cached application state only. Selection and filter-dimming styles take priority.

`KeepColor: true` drops that selection priority: a row keeps its own color under the
cursor and the frame's tinted left rule alone marks the selection. Use it when the colors
say something the reader most wants on the row they are pointing at (a git status) rather
than a type the accent can safely mask. A kept row with no color of its own is drawn in the
normal foreground, not the accent, so the list reads uniformly. Filter-dimming is
unaffected. The underlying contract is `core.KeepColorItem`, which any list row can
implement — `CompactDelegate` and `ColorDelegate` both honor it.
