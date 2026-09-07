package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTabBarOverflowAndClicks(t *testing.T) {
	p := NewTabBar()
	items := []TabItem{{ID: "a", Label: "alpha"}, {ID: "b", Label: "beta"}, {ID: "c", Label: "gamma"}}
	p.SetItems(items)
	p.SetActive("a")
	p.SetSize(15, 1)
	if got := ansi.Strip(p.View(false)); got != "  alpha  beta ›" {
		t.Fatalf("row = %q", got)
	}
	if id, hit := p.Click(9, 0); !hit || id != "b" {
		t.Fatalf("second tab click = %q, %v", id, hit)
	}
	if id, hit := p.Click(14, 0); !hit || id != "" {
		t.Fatal("arrow activated a tab")
	}
	if p.active != "a" || p.first != 1 {
		t.Fatal("scroll changed selection or did not advance")
	}
	// A routine refresh must not undo manual scrolling.
	p.SetItems(items)
	p.SetActive("a")
	if id, _ := p.Click(2, 0); id != "b" {
		t.Fatalf("scrolled click = %q", id)
	}
	p.SetActive("c")
	if !strings.Contains(ansi.Strip(p.View(false)), "gamma") {
		t.Fatal("active tab is hidden")
	}
	p.Click(0, 0)
	if p.first != 0 {
		t.Fatal("left arrow did not scroll back")
	}
	p.SetSize(8, 1)
	if id, _ := p.Click(3, 0); id != "c" {
		t.Fatalf("resize failed to reveal active tab: %q", id)
	}
	p.SetSize(40, 1)
	if id, _ := p.Click(1, 0); id != "a" {
		t.Fatal("wide view did not restore all tabs")
	}
	for _, xy := range [][2]int{{-1, 0}, {40, 0}, {1, 1}} {
		if _, hit := p.Click(xy[0], xy[1]); hit {
			t.Fatalf("outside click handled: %v", xy)
		}
	}
}

func TestTabBarUnicodeMarkersAndTinyWidths(t *testing.T) {
	p := NewTabBar()
	p.SetItems([]TabItem{{ID: "a", Label: "文書 é long filename", Marker: " (*) [P]"}, {ID: "b", Label: "next"}})
	p.SetActive("a")
	for width := 0; width <= 40; width++ {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			p.SetSize(width, 1)
			row := p.View(false)
			if got := ansi.StringWidth(row); got != width {
				t.Fatalf("width = %d, want %d: %q", got, width, row)
			}
			if strings.Contains(row, "\n") {
				t.Fatal("bar wrapped")
			}
			if width >= 13 && !strings.Contains(ansi.Strip(row), "(*) [P]") {
				t.Fatalf("markers truncated: %q", row)
			}
		})
	}
	p.SetItems(nil)
	if id, hit := p.Click(2, 0); id != "" || !hit {
		t.Fatal("empty bar click")
	}
	p.SetSize(40, 0)
	if p.View(false) != "" {
		t.Fatal("zero-height bar rendered")
	}
}

func TestTabBarIdentityChanges(t *testing.T) {
	p := NewTabBar()
	p.SetSize(11, 1)
	p.SetItems([]TabItem{{ID: "1", Label: "same"}, {ID: "2", Label: "same"}})
	p.SetActive("2")
	if id, _ := p.Click(3, 0); id != "2" {
		t.Fatal("duplicate labels confused identity")
	}
	p.SetItems([]TabItem{{ID: "1", Label: "renamed", Marker: " (*)"}})
	p.SetActive("1")
	if id, _ := p.Click(3, 0); id != "1" {
		t.Fatal("removal left stale hit spans")
	}
}
