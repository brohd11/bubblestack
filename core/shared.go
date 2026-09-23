package core

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
)

// Shared is the cross-cutting state the router owns and passes to every screen: the
// app's context (App, read with App[T]), terminal size, spinner, static help model and
// optional Chrome. Per-screen state (a task's channel) belongs on the screen.
type Shared struct {
	App    any     // consumer-owned context; recover it with App[T]
	Chrome *Chrome // optional header/status/output furniture (nil ⇒ fullscreen)
	// SaveListDensity persists density toggles for participating apps (set by Run); nil
	// means no persistence. A save failure does not block the change.
	SaveListDensity func(compact bool) error

	width  int
	height int
	bodyY  int // rows of chrome above the body; the router maintains it each resize

	Spinner spinner.Model
	help    help.Model // renders static (non-list) help bars

	// chrome memoizes the rendered furniture for the message being handled — see
	// chromeCache. The router owns it; nothing outside core touches it.
	chrome chromeCache
}

func NewShared(app any) *Shared {
	sp := spinner.New()
	sp.Spinner = spinner.Points

	h := help.New()

	return &Shared{
		App:     app,
		Spinner: sp,
		help:    h,
	}
}

// Log appends a line to the output pane when it supports logging, and is a no-op
// otherwise.
func (s *Shared) Log(line string, forceShow ...bool) {
	if s.Chrome == nil || s.Chrome.Output == nil {
		return
	}
	if l, ok := s.Chrome.Output.(interface{ Log(string, bool) }); ok {
		force := GetOptional(true, forceShow...)
		l.Log(line, force)
	}
}

// WriteStatus sets the status line directly (core.SetStatus also arms the clear timer).
// Optional flags: log (default false), forceShow (default false). No-op without chrome.
func (s *Shared) WriteStatus(line string, logParams ...bool) {
	if s.Chrome == nil {
		return
	}
	if s.Chrome.Status != nil {
		s.Chrome.Status.Set(line) // Set("") clears (Shown() == false)
	}
	var log = GetOptionalIdx(false, 0, logParams...)
	if log && line != "" {
		var forceShow = GetOptionalIdx(false, 1, logParams...)
		s.Log(line, forceShow)
	}
}

func (s *Shared) ClearStatus() {
	if s.Chrome != nil && s.Chrome.Status != nil {
		s.Chrome.Status.Clear()
	}
}

// SetStatus sets the status line only (no log) — the thin wrapper the existing call
// sites use. Migrate selected sites to WriteStatus(line, true) to also log the line.
func (s *Shared) SetStatus(msg string) { s.WriteStatus(msg) }

// App returns the consumer's context as *T: c := core.App[MyCtx](sh).
func App[T any](s *Shared) *T { return s.App.(*T) }

// Width reports the current terminal width, so a Header closure can size/truncate
// its content to fit (see HeaderInnerWidth).
func (s *Shared) Width() int { return s.width }

// BodyY is the terminal row where the body starts. Mouse rows are absolute, so a screen
// hit-testing its own layout subtracts it.
func (s *Shared) BodyY() int { return s.bodyY }
