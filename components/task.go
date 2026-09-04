package components

import (
	"context"
	"fmt"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// RunFunc executes a streaming background task: report() pipes progress lines into
// the log, and the terminating core.TaskEvent (Done:true) is sent on done. ctx is
// cancelled when the user aborts the task (esc), so a cancellable RunFunc should
// thread it into its network/process work and return promptly once it fires.
type RunFunc func(ctx context.Context, sh *core.Shared, report func(string, ...any), done chan<- core.TaskEvent)

// TaskScreen runs a streaming background task and shows its log. It is context-
// agnostic: the calling tab supplies run (the work), onDone (what to do with the
// terminating event), and — for tasks that stay on the log until dismissed — a
// doneLabel + onDismiss. It names no domain type.
//
// While the task runs, esc requests an abort: the run's context is cancelled and the
// screen waits for the run to unwind, then stays on the log showing "aborted" until
// the user dismisses it (rather than running onDone's success navigation).
//
// A router-level stack reset (resetToRoot/showTab) can drop a running TaskScreen
// without cancelling its context; the stream machinery (see startTask) is built so
// that can never wedge the worker goroutine.
type TaskScreen struct {
	label, doneLabel string
	Crumb            string
	CrumbShort       string // optional short breadcrumb segment; defaults to label
	Dir              string // directory this task concerns; enables the global Terminal key (DirLocator)
	stay             bool
	run              RunFunc
	onDone           func(*core.Shared, core.TaskEvent) core.Action
	onDismiss        func(*core.Shared) core.Action
	done             bool
	cancel           context.CancelFunc
	aborting         bool
	events           chan core.TaskEvent // the task's own event stream, created in Init
}

var _ core.Crumber = (*TaskScreen)(nil)
var _ core.DirLocator = (*TaskScreen)(nil)

// CrumbLabel contributes the task's label as its breadcrumb segment.
func (s *TaskScreen) CrumbLabel(short bool) string {
	return CrumbSegment(short, s.CrumbShort, "Task", "Task")
}

// LocateDir reports the directory this task concerns (Dir), so the global Terminal key
// opens a terminal there — the point where a failed git op tells the user to resolve it in a
// terminal. Empty dir ⇒ no locator (the key falls through).
func (s *TaskScreen) LocateDir() (string, bool) { return s.Dir, s.Dir != "" }

// NewTask builds a task that navigates away as soon as it finishes (install,
// install-all): onDone returns the navigation Action for the terminating event.
func NewTask(label string, run RunFunc, onDone func(*core.Shared, core.TaskEvent) core.Action) *TaskScreen {
	return &TaskScreen{label: label, run: run, onDone: onDone}
}

// NewStayTask builds a task that stays on the log after finishing (archive) until
// the user dismisses it: onDone records the result, onDismiss runs on esc/enter.
func NewStayTask(label, doneLabel string, run RunFunc,
	onDone func(*core.Shared, core.TaskEvent) core.Action, onDismiss func(*core.Shared) core.Action) *TaskScreen {
	return &TaskScreen{label: label, doneLabel: doneLabel, stay: true, run: run, onDone: onDone, onDismiss: onDismiss}
}

func (s *TaskScreen) Init(sh *core.Shared) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	// Buffered, so a burst of report lines (or a screen dropped by a router reset,
	// which stops draining) can't block the worker; see startTask.
	s.events = make(chan core.TaskEvent, 16)
	return startTask(ctx, sh, s.events, s.run)
}

func (s *TaskScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	switch msg := msg.(type) {
	case core.TaskEvent:
		if !msg.Done {
			sh.Log(msg.Line)
			return s, core.Async(waitForEvent(s.events))
		}
		s.done = true
		if s.aborting {
			// The run unwound after the abort. Stay on the log with an "aborted"
			// notice until the user dismisses it, rather than running onDone's
			// success navigation (which would land as if the work had completed).
			return s, core.SetStatusAndLog("aborted")
		}
		act := s.onDone(sh, msg)
		// A non-stay task navigates away via act (e.g. a ShowTab). A stay-task remains
		// on its log until the user dismisses it (esc/enter), but act is still applied
		// — it's expected to be non-navigational for a stay-task (e.g. a broadcast that
		// reloads another tab), so returning s keeps the screen on top.
		return s, act

	case tea.KeyPressMsg:
		k := msg.String()
		// While the task is still running, esc requests an abort: cancel the run's
		// context and wait for its terminating event (handled above) to unwind it.
		if !s.done && !s.aborting && core.MatchKey(k, core.Keys.Back) {
			s.aborting = true
			if s.cancel != nil {
				s.cancel()
			}
			return s, core.SetStatusAndLog("aborting…")
		}
		if s.done && (core.MatchKey(k, core.Keys.Back) || core.MatchKey(k, core.Keys.Select)) {
			return s.dismiss(sh)
		}

	case tea.MouseClickMsg:
		// A click dismisses a finished task, the same as esc/enter — a done task
		// is a dead end whose only moves are exits. Clicks while it runs do
		// nothing: aborting work is esc's job, and a click that cancelled would
		// be a landmine.
		if msg.Button == tea.MouseLeft &&
			s.done && (s.aborting || s.stay) {
			return s.dismiss(sh)
		}
	}
	return s, core.Action{}
}

// dismiss is the shared exit from a finished task (esc/enter key, or a click).
// An aborted task (any kind) and a finished stay-task both linger on the log
// until dismissed. Aborted tasks fall back to a plain Pop when the caller
// supplied no onDismiss (non-stay tasks); anything else is a no-op — a done
// non-stay task already navigated away.
func (s *TaskScreen) dismiss(sh *core.Shared) (core.Screen, core.Action) {
	if s.aborting && s.onDismiss == nil {
		return s, core.Pop()
	}
	if s.aborting || s.stay {
		return s, s.onDismiss(sh)
	}
	return s, core.Action{}
}

// View renders just the spinner/progress line; the streaming log is drawn by the
// router as shared output chrome below it.
func (s *TaskScreen) View(sh *core.Shared) string {
	glyph := sh.Spinner.View()
	if s.done {
		glyph = "•"
	}
	label := s.label
	switch {
	case s.aborting && s.done:
		label = "aborted — esc to go back"
	case s.aborting:
		label = "aborting…"
	case s.stay && s.done:
		label = s.doneLabel
	}
	return fmt.Sprintf("\n  %s %s", glyph, label)
}

func (s *TaskScreen) HelpView(sh *core.Shared) string {
	// A task with a directory is a DirLocator, so the terminal/open-dir keys still fire on it —
	// what the "resolve it in a terminal (t)" failure message counts on. They are deliberately
	// NOT on this bar: the bar stays sparse (see core.ShortHelp), and a task screen has no (?)
	// help to put them in, so they go unadvertised here rather than crowding four entries in.
	if s.done && (s.aborting || s.stay) {
		return sh.BindingHelp([]key.Binding{core.Hint("back", core.Keys.Back)})
	}
	if s.aborting {
		return sh.NoteHelp("aborting…")
	}
	return sh.BindingHelp([]key.Binding{core.Hint("abort", core.Keys.Back)})
}

func (s *TaskScreen) SetSize(sh *core.Shared, width, bodyHeight int) {}

// ---------- streaming task pump ----------

// startTask spawns run in the background, piping report() lines into the output
// log via the screen's own events channel, and returns the spinner tick + the wait
// for the first event. The channel belongs to the TaskScreen (created in Init), not
// to Shared: two tasks in flight at once must never retarget each other's stream.
//
// Every send the worker can make is wedge-proof: a router reset can drop the screen
// without cancelling ctx (esc was never pressed), and from then on nothing drains
// events — a blocking send would leak the worker goroutine mid-task.
func startTask(ctx context.Context, sh *core.Shared, events chan core.TaskEvent, run RunFunc) tea.Cmd {
	go func() {
		report := func(format string, args ...any) {
			// Non-blocking: a live screen's pending wait takes the line; a full
			// buffer (UI a tick behind, or screen gone) drops it — losing a
			// progress line beats wedging the worker.
			select {
			case events <- core.TaskEvent{Line: fmt.Sprintf(format, args...)}:
			case <-ctx.Done():
			default:
			}
		}
		// done is private and buffered, so the run's terminating send never
		// blocks either; getting it onto events is this pump goroutine's job.
		done := make(chan core.TaskEvent, 1)
		run(ctx, sh, report, done)
		// run has returned, so its terminating send — if it made one — is already
		// buffered in done: a non-blocking receive is race-free (unlike selecting
		// against ctx.Done(), which could win the coin flip during an abort and
		// strand the screen waiting for the unwind that already happened).
		select {
		case ev := <-done:
			// The terminating event must reach a live screen — it drives onDone
			// and the aborted state — so it is never dropped. No report can race
			// us for buffer space (run has returned): if the buffer is full
			// (dropped screen, or UI a tick behind), sacrifice the oldest
			// buffered progress line to make room instead of blocking.
			select {
			case events <- ev:
			default:
				select {
				case <-events:
				default:
				}
				events <- ev
			}
		default:
			// The run returned without its terminating event; nothing to forward.
		}
	}()
	return tea.Batch(sh.Spinner.Tick, waitForEvent(events))
}

func waitForEvent(events chan core.TaskEvent) tea.Cmd {
	return func() tea.Msg { return <-events }
}
