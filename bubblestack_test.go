package bubblestack

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sgrLead() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true}
}

func runeMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestMouseSGRFragmentFilterDropsFragmentPair(t *testing.T) {
	filter := mouseSGRFragmentFilter()
	if got := filter(nil, sgrLead()); got != nil {
		t.Fatalf("SGR lead passed through as %#v", got)
	}
	// A timer/mouse/application message can be queued between the two input fragments.
	marker := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	if got := filter(nil, marker); got != marker {
		t.Fatalf("interleaved non-key message = %#v, want %#v", got, marker)
	}
	if got := filter(nil, runeMsg("<64;53;18M")); got != nil {
		t.Fatalf("SGR tail passed through as %#v", got)
	}

	// Release reports use lowercase m and must be caught too.
	filter = mouseSGRFragmentFilter()
	filter(nil, sgrLead())
	if got := filter(nil, runeMsg("<0;12;9m")); got != nil {
		t.Fatalf("SGR release tail passed through as %#v", got)
	}
}

func TestMouseSGRFragmentFilterPreservesOtherInput(t *testing.T) {
	filter := mouseSGRFragmentFilter()

	standalone := runeMsg("<64;53;18M")
	if got := filter(nil, standalone); !reflect.DeepEqual(got, standalone) {
		t.Fatalf("unarmed SGR-shaped text = %#v, want %#v", got, standalone)
	}
	altRune := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true}
	if got := filter(nil, altRune); !reflect.DeepEqual(got, altRune) {
		t.Fatalf("unrelated Alt key = %#v, want %#v", got, altRune)
	}

	// A non-matching next key disarms the lead and passes through; a later SGR-shaped
	// key is therefore ordinary input rather than an indefinitely armed false positive.
	if got := filter(nil, sgrLead()); got != nil {
		t.Fatalf("SGR lead passed through as %#v", got)
	}
	plain := runeMsg("hello")
	if got := filter(nil, plain); !reflect.DeepEqual(got, plain) {
		t.Fatalf("non-SGR key after lead = %#v, want %#v", got, plain)
	}
	if got := filter(nil, standalone); !reflect.DeepEqual(got, standalone) {
		t.Fatalf("filter stayed armed after a non-match: %#v", got)
	}
}
