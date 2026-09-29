package core

import (
	"image/color"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// The resolved palette and derived styles. applyTheme resolves the active theme's pairs
// into these colors and rebuildStyles derives the styles; init applies the default. They
// are resolved colors so they work directly with lipgloss.
var (
	MutedColor     color.Color
	logColor       color.Color
	BorderColor    color.Color
	FocusedColor   color.Color
	OnFocusedColor color.Color // text drawn on the accent (title bar)
	SelectionColor color.Color // background of a SelectBackground list row

	statusStyle lipgloss.Style
	logStyle    lipgloss.Style

	// Tab strip: active tab accented, others muted, a rule below.
	tabStripStyle  lipgloss.Style
	activeTabStyle lipgloss.Style
	tabStyle       lipgloss.Style
	tabRuleStyle   lipgloss.Style

	// Breadcrumb bar: upstream segments muted, the current one accented.
	breadcrumbBarStyle  lipgloss.Style
	breadcrumbRuleStyle lipgloss.Style
	crumbMutedStyle     lipgloss.Style
	crumbCurStyle       lipgloss.Style

	boxStyle    lipgloss.Style
	headerStyle lipgloss.Style
	labelStyle  lipgloss.Style

	// listStyles are bubbles' list styles, reused for title bars and static help so they
	// match real lists.
	listStyles list.Styles
)

func init() { applyTheme(current) }

// rebuildStyles rebuilds the derived styles from the current palette. applyTheme
// calls it after swapping colors so a theme switch repaints every chrome element.
func rebuildStyles() {
	statusStyle = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(FocusedColor)
	logStyle = lipgloss.NewStyle().Foreground(logColor)

	tabStripStyle = lipgloss.NewStyle().Padding(0, 1)
	activeTabStyle = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(FocusedColor)
	tabStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(MutedColor)
	tabRuleStyle = lipgloss.NewStyle().Foreground(BorderColor)

	breadcrumbBarStyle = lipgloss.NewStyle().Padding(0, 1)
	breadcrumbRuleStyle = lipgloss.NewStyle().Foreground(BorderColor)
	crumbMutedStyle = lipgloss.NewStyle().Foreground(MutedColor)
	crumbCurStyle = lipgloss.NewStyle().Foreground(FocusedColor).Bold(true)

	// No top margin: the gap above the box is owned by WithTitle so the body can
	// hug the top when no title bar is rendered. Margin order is top,right,bottom,left.
	boxStyle = lipgloss.NewStyle().Margin(0, 2, 1, 2).Padding(1, 2).Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(BorderColor)
	labelStyle = lipgloss.NewStyle().Foreground(MutedColor)

	// Reset the list styles and theme the title bar with the accent; OnFocusedColor keeps its
	// text readable.
	listStyles = list.DefaultStyles(isDark)
	listStyles.Title = listStyles.Title.Background(FocusedColor).Foreground(OnFocusedColor)
}

// MutedStyle is the themed muted-foreground style (labels, hints, secondary text).
func MutedStyle() lipgloss.Style { return labelStyle }

// AccentStyle is the themed bold accent style (the current item, an active toggle).
func AccentStyle() lipgloss.Style { return crumbCurStyle }

// LogStyle is the themed style for log text, read at render time.
func LogStyle() lipgloss.Style { return logStyle }

// StatusStyle is the themed style for the status line, read at render time.
func StatusStyle() lipgloss.Style { return statusStyle }
