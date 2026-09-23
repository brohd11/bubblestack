package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// RootListOpts configures a permanent tab's list. Picker options apply, except
// that Back does not pop and an unhandled OnKey falls through to the row's Keys.
type RootListOpts struct {
	PickerOpts
	Init    func(*core.Shared) tea.Cmd
	Receive func(*core.Shared, any) core.Action
	// CompactState shares an app-owned density preference, overriding Compact and the app's
	// ListDensityProvider; toggles write through. Keep it across root rebuilds.
	CompactState *bool
}

// RootListScreen is a picker-backed tab root: list interaction, density, theme and
// geometry, with app work in Init, OnKey, Refresh and Receive.
type RootListScreen struct {
	*PickerScreen
	init    func(*core.Shared) tea.Cmd
	receive func(*core.Shared, any) core.Action
}

var _ core.Screen = (*RootListScreen)(nil)
var _ core.Receiver = (*RootListScreen)(nil)

func NewRootList(items []list.Item, opts RootListOpts) *RootListScreen {
	if opts.Crumb == "" {
		opts.Crumb = "Tab"
	}
	if opts.CompactState != nil {
		opts.Compact = *opts.CompactState
	}
	p := newPicker(items, opts.PickerOpts, true)
	p.compactState = opts.CompactState
	return &RootListScreen{PickerScreen: p, init: opts.Init, receive: opts.Receive}
}

func (s *RootListScreen) Init(sh *core.Shared) tea.Cmd {
	s.PickerScreen.Init(sh)
	if s.init != nil {
		return s.init(sh)
	}
	return nil
}

func (s *RootListScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	// Return the root, not the embedded picker: otherwise the router would lose
	// the root's lifecycle callbacks and navigation behavior after one message.
	return s, s.PickerScreen.update(sh, msg, true)
}

func (s *RootListScreen) Receive(sh *core.Shared, payload any) core.Action {
	s.PickerScreen.Receive(sh, payload)
	if _, ok := payload.(core.MsgListDensityChanged); ok {
		return core.Action{}
	}
	if s.receive != nil {
		return s.receive(sh, payload)
	}
	return core.Action{}
}

func (s *RootListScreen) HelpView(*core.Shared) string {
	return core.ShortHelp(s.list, core.HelpTabbed)
}
