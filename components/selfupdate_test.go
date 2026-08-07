package components

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// The self-update tests drive the loading screen's Update directly with fake check
// results, skipping Init so no fetch goroutine is involved — the error/up-to-date/
// available branching in onResult is pure. The hooks are fakes; no network.

func fakeHooks(info SelfUpdateInfo, err error) SelfUpdateHooks {
	return SelfUpdateHooks{
		AppName: "testapp",
		Check:   func(context.Context) (SelfUpdateInfo, error) { return info, err },
		Apply:   func(context.Context, SelfUpdateInfo, func(string, ...any)) error { return nil },
	}
}

func TestSelfUpdateLoadingCheckError(t *testing.T) {
	s := NewSelfUpdateLoading(fakeHooks(SelfUpdateInfo{}, errors.New("boom")))
	_, act := s.Update(core.NewShared(nil), selfUpdateInfoMsg{err: errors.New("boom")})
	want := core.Seq(core.SetStatusAndLog("update check failed: boom"), core.Pop())
	if !reflect.DeepEqual(act, want) {
		t.Errorf("a failed check should report and pop, got %+v", act)
	}
}

func TestSelfUpdateLoadingUpToDate(t *testing.T) {
	s := NewSelfUpdateLoading(fakeHooks(SelfUpdateInfo{Current: "v1.0.0", LatestTag: "v1.0.0"}, nil))
	_, act := s.Update(core.NewShared(nil), selfUpdateInfoMsg{info: SelfUpdateInfo{Available: false}})
	want := core.Seq(core.SetStatus("testapp is up to date"), core.Pop())
	if !reflect.DeepEqual(act, want) {
		t.Errorf("no available update should report up to date and pop, got %+v", act)
	}
}

func TestSelfUpdateLoadingAvailableOpensConfirm(t *testing.T) {
	info := SelfUpdateInfo{Current: "v1.0.0", LatestTag: "v1.1.0", Available: true}
	s := NewSelfUpdateLoading(fakeHooks(info, nil))
	_, act := s.Update(core.NewShared(nil), selfUpdateInfoMsg{info: info})
	// The result is a Replace onto the confirm; the confirm screen itself is
	// unexported inside core's replaceMsg, so assert the navigation type.
	if got := reflect.TypeOf(act.Msg).String(); got != "core.replaceMsg" {
		t.Errorf("an available update should replace onto the confirm, got %s", got)
	}
}

func TestSelfUpdateLoadingUnrelatedMsgIgnored(t *testing.T) {
	s := NewSelfUpdateLoading(fakeHooks(SelfUpdateInfo{}, nil))
	_, act := s.Update(core.NewShared(nil), fetchResult{ok: true})
	if !reflect.DeepEqual(act, core.Action{}) {
		t.Errorf("an unrecognized message should be inert, got %+v", act)
	}
}

// TestSelfUpdateLoadingRunThreadsTimeout checks the loading screen's Run builds a cmd
// that calls Check with a deadline-bearing context and wraps its result.
func TestSelfUpdateLoadingRunThreadsTimeout(t *testing.T) {
	var sawDeadline bool
	hooks := fakeHooks(SelfUpdateInfo{Available: true}, nil)
	hooks.Check = func(ctx context.Context) (SelfUpdateInfo, error) {
		_, sawDeadline = ctx.Deadline()
		return SelfUpdateInfo{Available: true}, nil
	}
	s := NewSelfUpdateLoading(hooks)
	msg := s.Run(context.Background())()
	if !sawDeadline {
		t.Error("Check should run under the check-timeout context")
	}
	m, ok := msg.(selfUpdateInfoMsg)
	if !ok {
		t.Fatalf("the fetch cmd should return a selfUpdateInfoMsg, got %T", msg)
	}
	if !m.info.Available {
		t.Error("the check result should ride the message back to onResult")
	}
}

func TestSelfUpdateCheckCmdSilentUnlessAvailable(t *testing.T) {
	cases := []struct {
		name string
		info SelfUpdateInfo
		err  error
	}{
		{"error", SelfUpdateInfo{}, errors.New("boom")},
		{"up to date", SelfUpdateInfo{Current: "v1.0.0", LatestTag: "v1.0.0"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := SelfUpdateCheckCmd(fakeHooks(tc.info, tc.err))(); msg != nil {
				t.Errorf("the startup check should be silent, got %v", msg)
			}
		})
	}
}

func TestSelfUpdateCheckCmdAnnouncesAvailable(t *testing.T) {
	info := SelfUpdateInfo{Current: "v1.0.0", LatestTag: "v1.1.0", Available: true}
	msg := SelfUpdateCheckCmd(fakeHooks(info, nil))()
	want := tea.Msg(core.SetStatusAndLog("testapp update available: v1.0.0 → v1.1.0 · Actions ▸ Update testapp", true))
	if !reflect.DeepEqual(msg, want) {
		t.Errorf("an available update should announce on the status line, got %v", msg)
	}
}
