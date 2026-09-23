package core

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// statusClearDelay is how long a status line stays up after the most recent write
// before the router's auto-clear timer hides it.
const statusClearDelay = 5 * time.Second

// TabEntry is one top-level tab: a strip title and a root constructor. Roots are built
// lazily and rebuilt on theme changes, which loses no state since they reflect on-disk
// data.
type TabEntry struct {
	Title string
	New   func(*Shared) Screen
}

// Router is the top-level tea.Model: it owns the chrome, the tabs and the active tab's
// navigation stack, draws the chrome around the active screen and applies navigation
// messages. The root is the permanent bottom of the stack. Tabs switch only at depth 1,
// so the stack always belongs to the active tab.
type Router struct {
	sh     *Shared
	tabs   []TabEntry
	roots  []Screen // cached root per tab, built from tabs[i].New(sh)
	active int      // index into tabs of the visible tab
	stack  []Screen // live nav stack for the active tab; stack[0] == roots[active]

	// statusGen is the status generation an auto-clear timer is armed for; a newer write arms
	// a new one.
	statusGen int

	// refreshAction backs the global Refresh key; nil leaves the key to the screen.
	refreshAction func(*Shared) Action

	// terminalAction backs the global Terminal key with the top screen's DirLocator
	// directory; nil leaves the key to the screen (keeping any row-level "t").
	terminalAction func(dir string) Action

	// terminalWindowAction backs the TerminalWindow key the same way.
	terminalWindowAction func(dir string) Action

	// openDirAction backs the OpenDir key the same way.
	openDirAction func(dir string) Action

	// appInit is an app-level startup command, batched with the initial screen's Init
	// in Init(). Consumer-set via SetInit; nil ⇒ no app-level startup command.
	appInit func(*Shared) tea.Cmd

	// mouseOn is whether the terminal reports mouse events. It starts on, which costs the
	// terminal's own drag-select; the Mouse key hands selection back at the cost of the wheel.
	mouseOn bool
}

// SetRefreshAction wires the global Refresh key; it fires from any depth unless text is
// being captured.
func (r *Router) SetRefreshAction(f func(*Shared) Action) { r.refreshAction = f }

// SetTerminalAction wires the global Terminal key, called with the top screen's
// DirLocator directory. nil leaves the key to the screen.
func (r *Router) SetTerminalAction(f func(dir string) Action) { r.terminalAction = f }

// SetTerminalWindowAction wires the detached-window variant of the Terminal key.
func (r *Router) SetTerminalWindowAction(f func(dir string) Action) { r.terminalWindowAction = f }

// SetOpenDirAction wires the file-manager key, resolved like SetTerminalAction.
func (r *Router) SetOpenDirAction(f func(dir string) Action) { r.openDirAction = f }

// SetInit wires the consumer's app-level startup command, batched with the initial
// screen's Init in Init(). Called by the Run facade after NewRouter.
func (r *Router) SetInit(f func(*Shared) tea.Cmd) { r.appInit = f }

func NewRouter(sh *Shared, tabs []TabEntry) Router {
	roots := make([]Screen, len(tabs))
	for i := range tabs {
		roots[i] = tabs[i].New(sh)
	}
	return Router{sh: sh, tabs: tabs, roots: roots, stack: []Screen{roots[0]}, mouseOn: true}
}

func (r Router) Top() Screen { return r.stack[len(r.stack)-1] }

func (r Router) Init() tea.Cmd {
	cmd := r.Top().Init(r.sh)
	if r.appInit == nil {
		return cmd
	}
	return tea.Batch(cmd, r.appInit(r.sh))
}

func (r Router) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global keys first, for whatever screen is on top. KeyPressMsg, not tea.KeyMsg: that
	// interface also covers key releases, which would fire every binding twice.
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if act, handled := r.globalKey(key); handled {
			cmd := r.settle(act)
			return r, cmd
		}
	}

	// A wheel over the output pane (router-owned chrome) is claimed here; everything else
	// falls through to the active screen.
	if m, ok := msg.(tea.MouseMsg); ok {
		if act, handled := r.mouse(m); handled {
			cmd := r.settle(act)
			return r, cmd
		}
	}

	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.BlurMsg, tea.FocusMsg:
		// Broadcast, then fall through so the top screen still sees it in Update. A drag's
		// owner or a screen that re-reads state on focus may be below the top.
		r.apply(PropagateAll(msg), &cmds)
	case tea.BackgroundColorMsg:
		// The terminal's OSC 11 answer picks each adaptive color's half; cached tab roots
		// baked their styles from the old guess, so they are rebuilt.
		if SetBackgroundIsDark(msg.IsDark()) {
			r.apply(RefreshRoots(), &cmds)
		}
		return r, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		r.sh.width, r.sh.height = msg.Width, msg.Height
		r.resize()
		return r, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		r.sh.Spinner, cmd = r.sh.Spinner.Update(msg)
		return r, cmd
	}

	// An Action or bare control message arriving via the queue is applied to the stack the
	// same way as one a screen returns.
	if act, ok := msg.(Action); ok {
		cmd := r.settle(act)
		return r, cmd
	}
	if _, ok := msg.(ctrlMsg); ok {
		r.resolveCtrl(msg, &cmds)
		r.resize()
		return r, tea.Batch(cmds...)
	}

	s, act := r.Top().Update(r.sh, msg)
	r.stack[len(r.stack)-1] = s
	r.apply(act, &cmds)
	// Re-lay-out after every message rather than tracking what changed content height.
	r.resize()
	return r, tea.Batch(cmds...)
}

// settle applies act, re-lays-out, and returns the batched async cmds.
func (r *Router) settle(act Action) tea.Cmd {
	var cmds []tea.Cmd
	r.apply(act, &cmds)
	r.resize()
	return tea.Batch(cmds...)
}

// apply unpacks an Action: it resolves the control-message lane against the stack
// (synchronously, this same tick) and appends the async cmd lane to cmds for bubbletea.
func (r *Router) apply(act Action, cmds *[]tea.Cmd) {
	r.resolveCtrl(act.Msg, cmds)
	if act.Cmd != nil {
		*cmds = append(*cmds, act.Cmd)
	}
}

// resolveCtrl applies a control message and any it cascades into (a broadcast whose
// Receivers request ShowTab) in one tick, collecting async cmds. nil is a no-op.
func (r *Router) resolveCtrl(m tea.Msg, cmds *[]tea.Cmd) {
	queue := []tea.Msg{m}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		if m == nil {
			continue
		}
		queue = append(queue, r.applyCtrl(m, cmds)...)
	}
}

// applyCtrl applies one control message to the stack, returning follow-up control
// messages and appending async cmds.
func (r *Router) applyCtrl(m tea.Msg, cmds *[]tea.Cmd) (follows []tea.Msg) {
	push := func(cmd tea.Cmd) {
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	}
	switch m := m.(type) {
	case pushMsg:
		r.stack = append(r.stack, m.s)
		push(m.s.Init(r.sh))

	case replaceMsg:
		r.stack[len(r.stack)-1] = m.s
		push(m.s.Init(r.sh))

	case popMsg:
		for i := 0; i < m.n && len(r.stack) > 1; i++ {
			r.stack = r.stack[:len(r.stack)-1]
		}

	case popToMsg:
		// Always leave the current screen, then stop at the first screen that opts
		// into PopStopper (a command hub), or the root.
		for len(r.stack) > 1 {
			r.stack = r.stack[:len(r.stack)-1]
			if s, ok := r.Top().(PopStopper); ok && s.PopStop() {
				break
			}
		}

	case resetToRootMsg:
		r.stack = r.stack[:1]

	case seqMsg:
		// Unpack each grouped Action: hand its control message back to resolveCtrl's
		// worklist (applied in order this same tick) and collect its async cmd.
		for _, a := range m.acts {
			if a.Msg != nil {
				follows = append(follows, a.Msg)
			}
			push(a.Cmd)
		}

	case showTabMsg:
		// Switch to the tab whose title matches and unwind it to its root. The router
		// addresses tabs only by the title it already renders — no separate identity.
		for i := range r.tabs {
			if r.tabs[i].Title == m.title {
				r.active = i
				r.stack = []Screen{r.roots[i]}
				break
			}
		}

	case propagateMsg:
		// Broadcast the payload to every tab root, the active stack and the App, each handling
		// what it recognizes; returned Actions resolve this tick. The App goes last, so its
		// follow-up lands after every screen has seen the payload.
		notify := func(v any) {
			if rc, ok := v.(Receiver); ok {
				act := rc.Receive(r.sh, m.payload)
				if act.Msg != nil {
					follows = append(follows, act.Msg)
				}
				push(act.Cmd)
			}
		}
		for i := range r.roots {
			notify(r.roots[i])
		}
		for _, s := range r.stack[1:] { // the active root is already covered via r.roots[active]
			notify(s)
		}
		notify(r.sh.App)

	case refreshRootsMsg:
		// Rebuild every tab root so it re-bakes its styles from the new palette (App.Receive →
		// RefreshRoots). Deeper screens are rebuilt when reopened.
		for i := range r.roots {
			r.roots[i] = r.tabs[i].New(r.sh)
		}
		r.stack[0] = r.roots[r.active]

	case statusSetMsg:
		if m.str != "" { // empty messages don't print, so don't start timer
			r.sh.WriteStatus(m.str, m.wrLog, m.forceShow)
			statCmd := r.getStatusClear()
			if statCmd != nil {
				push(statCmd)
			}
		}
	case statusClearMsg:
		// Clear the status only if no newer write has advanced its generation since this timer
		// was armed.
		ch := r.sh.Chrome
		if ch != nil && ch.Status != nil && ch.Status.Gen() == m.gen {
			ch.Status.Clear()
		}
	}
	return follows
}

// getStatusClear returns the timer that clears the status line after a statusSetMsg.
func (r *Router) getStatusClear() tea.Cmd {
	ch := r.sh.Chrome
	if ch == nil || ch.Status == nil {
		return nil
	}
	g := ch.Status.Gen()
	if g == r.statusGen {
		return nil
	}
	r.statusGen = g
	if !ch.Status.Shown() {
		return nil
	}
	return tea.Tick(statusClearDelay, func(time.Time) tea.Msg {
		return statusClearMsg{gen: g}
	})
}
