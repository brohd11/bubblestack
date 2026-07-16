# bubblestack

A small, reusable [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework:
a router over a screen stack, plus context-agnostic, closure-configured components. It names
no application domain type — a consumer supplies its context (recovered via `core.App[T]`),
optional header/output/status chrome, a theme, and its tabs, and `bubblestack.Run` wires the rest.

- **`core/`** — `Shared` state, the `Router` and navigation `Action`s (Push/Pop/Replace/…),
  the `Screen` interface and optional capabilities (Receiver/Crumber/Overlayer/…), a theme
  registry, and layout/help/style helpers.
- **`components/`** — reusable screens configured by closures: a self-dispatching list `Item`,
  `PickerScreen`, `DialogScreen`, `LoadingScreen`, `TaskScreen`, `FormScreen`, `DocScreen`, and
  the default `LogPane`/`StatusLine`.

Used by [gdaddon](https://github.com/brohd11/gdaddon) and
[repoview](https://github.com/brohd11/repoview).

```go
import "github.com/brohd11/bubblestack"
```
