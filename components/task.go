package components

import (
	"context"
	"fmt"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// RunFunc runs a streaming background task: report sends progress lines to the log, and
// the terminating core.TaskEvent (Done: true) goes on done. ctx is cancelled on abort
// (esc); cancellable work should honor it.
type RunFunc func(ctx context.Context, sh *core.Shared, report func(string, ...any), done chan<- core.TaskEvent)

// TaskScreen runs a streaming task and shows its log. The caller supplies run, onDone,
// and, for tasks that stay on the log, a doneLabel and onDismiss. While running, esc
// aborts: the context is cancelled and the screen stays on the log showing "aborted"
// until dismissed. A stack reset can drop a running TaskScreen without cancelling it;
// startTask is built so the worker can never block on that.
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

// LocateDir reports Dir, so the terminal key opens where a failed git op says to resolve
// things.
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
			// The run unwound after an abort: stay on the log instead of running onDone's success
			// navigation.
			return s, core.SetStatusAndLog("aborted")
		}
		act := s.onDone(sh, msg)
		// A non-stay task navigates away via act. A stay-task stays until dismissed, but act (a
		// non-navigational broadcast, say) is still applied.
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
		// A click dismisses a finished task, like esc/enter. Clicks while running do nothing:
		// aborting is esc's job.
		if msg.Button == tea.MouseLeft &&
			s.done && (s.aborting || s.stay) {
			return s.dismiss(sh)
		}
	}
	return s, core.Action{}
}

// dismiss exits a finished task. Aborted tasks and finished stay-tasks linger until
// dismissed; an aborted task without onDismiss pops. Otherwise it is a no-op.
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
	// A task with Dir fires the terminal keys (the failure message relies on it) but they stay
	// off this sparse bar (see core.ShortHelp).
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

// startTask runs the task in the background, piping report lines through the screen's own
// events channel (so concurrent tasks never mix streams), and returns the spinner tick
// plus the first wait. Every worker send is non-blocking: a stack reset can drop the
// screen without cancelling ctx, leaving nothing to drain the channel.
func startTask(ctx context.Context, sh *core.Shared, events chan core.TaskEvent, run RunFunc) tea.Cmd {
	go func() {
		report := func(format string, args ...any) {
			// Non-blocking: drop a progress line rather than block the worker on a full buffer.
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
		// run has returned, so its final event (if any) is already buffered; a non-blocking
		// receive cannot race the abort path.
		select {
		case ev := <-done:
			// The final event must reach a live screen, so it is never dropped: if the buffer is
			// full, evict the oldest progress line to make room.
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
