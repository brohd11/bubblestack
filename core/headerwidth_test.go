package core

import "testing"

// HeaderValueWidth replaced three hand-computed literals. These pin the arithmetic the
// apps used to spell out: HeaderInnerWidth, minus headerStyle's padding, minus the label.
func TestHeaderValueWidth(t *testing.T) {
	const width = 80
	inner := HeaderInnerWidth(width) // 80 - 4 (border 2 + padding 2)

	tests := []struct {
		label string
		want  int
	}{
		{"Root:  ", inner - 2 - 7},     // repoview's old "inner - 9"
		{"Root:   ", inner - 2 - 8},    // golaunch's old "inner - 10"
		{"Manifest: ", inner - 2 - 10}, // gdaddon's: it had subtracted only the label
	}
	for _, tt := range tests {
		if got := HeaderValueWidth(width, tt.label); got != tt.want {
			t.Errorf("HeaderValueWidth(%d, %q) = %d, want %d", width, tt.label, got, tt.want)
		}
	}
}

// gdaddon's literal omitted the padding, so its values were two columns wider than the
// box could show. The shared helper is what makes that unrepeatable.
func TestHeaderValueWidthAccountsForPadding(t *testing.T) {
	const width = 80
	labelOnly := HeaderInnerWidth(width) - len("Manifest: ")
	if got := HeaderValueWidth(width, "Manifest: "); got != labelOnly-2 {
		t.Errorf("HeaderValueWidth = %d; want %d (two narrower than label-only %d)", got, labelOnly-2, labelOnly)
	}
}

// A label wider than the box must not yield a negative or absurd budget.
func TestHeaderValueWidthFloor(t *testing.T) {
	if got := HeaderValueWidth(10, "an extremely long label that cannot fit"); got != 4 {
		t.Errorf("floor = %d, want 4", got)
	}
}

// Wide runes count as the columns they occupy, which len() would get wrong.
func TestHeaderValueWidthMeasuresColumnsNotBytes(t *testing.T) {
	const width = 80
	wide := HeaderValueWidth(width, "日本: ")     // 2 wide runes (4 cols) + ": " = 6 cols
	narrow := HeaderValueWidth(width, "abcdef") // 6 cols
	if wide != narrow {
		t.Errorf("wide-rune label gave %d, ascii label of equal width gave %d", wide, narrow)
	}
}
