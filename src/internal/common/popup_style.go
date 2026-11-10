package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PopupStyleSet is how the s list colors its text, on the file panel's
// background it is drawn in, each kind its own color from the theme: the
// title as the help menu's, keys as its keys, section labels as a panel's
// path, sessions in Claude's orange, names in a panel's text, the picked
// name brighter, where things are dimmer, numbers and scores dimmer still.
type PopupStyleSet struct {
	Accent, Title, Section, Session, Rule, Text, Picked, Value, Muted, Faint, Key lipgloss.Style
}

// PopupBorderColor is the popups' frame.
func PopupBorderColor() lipgloss.TerminalColor { return FilePanelBorderActiveColor }

// PopupStyles are the s list's styles on a file panel's background, the
// panel it is drawn in, or on the picked row's bar when picked.
func PopupStyles(picked bool) PopupStyleSet {
	var bg lipgloss.TerminalColor = FilePanelBGColor
	if picked {
		bg = PopupPickedBG()
	}
	on := func(fg lipgloss.TerminalColor) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(fg).Background(bg)
	}
	return PopupStyleSet{
		Accent:  on(FilePanelBorderActiveColor),
		Title:   on(helpMenuTitleColor).Bold(true),
		Section: on(filePanelTopPathColor).Bold(true),
		Session: on(lipgloss.Color(ClaudeAgentColor)),
		Rule:    on(FilePanelBorderColor),
		Text:    on(FilePanelFGColor),
		Picked:  on(sidebarItemSelectedFGColor).Bold(true),
		Value:   on(FilePanelFGColor).Bold(true),
		Muted:   on(blendOr(FilePanelFGColor, FilePanelBGColor, 0.35, FilePanelBorderColor)), //nolint:mnd // a third toward the background
		Faint:   on(blendOr(FilePanelFGColor, FilePanelBGColor, 0.55, FilePanelBorderColor)), //nolint:mnd // half way, and more
		Key:     on(helpMenuHotkeyColor),
	}
}

// PopupPickedBG is the picked row's bar: the file panel's cursor bar, or,
// for a theme without one, the background a little toward the text.
func PopupPickedBG() lipgloss.TerminalColor {
	if FilePanelCursorRowBGColor != "" {
		return FilePanelCursorRowBGColor
	}
	return blendOr(FilePanelBGColor, FilePanelFGColor, 0.12, FilePanelBGColor) //nolint:mnd // a faint bar
}

// PopupHasBar reports whether the picked row's bar shows: a transparent
// theme has none, and marks the row with the cursor instead.
func PopupHasBar() bool {
	return PopupPickedBG() != lipgloss.TerminalColor(FilePanelBGColor)
}

// blendOr is a t of the way from a to b, or or when either isn't a
// "#rrggbb" color (a transparent background, a terminal palette number).
func blendOr(a, b lipgloss.TerminalColor, t float64, or lipgloss.TerminalColor) lipgloss.TerminalColor {
	ca, okA := hexRGB(a)
	cb, okB := hexRGB(b)
	if !okA || !okB {
		return or
	}
	mix := func(x, y int) int { return int(float64(x) + (float64(y)-float64(x))*t + 0.5) } //nolint:mnd // rounding
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", mix(ca[0], cb[0]), mix(ca[1], cb[1]), mix(ca[2], cb[2])))
}

func hexRGB(c lipgloss.TerminalColor) ([3]int, bool) {
	s, ok := c.(lipgloss.Color)
	hex := strings.TrimPrefix(string(s), "#")
	if !ok || len(hex) != 6 || !strings.HasPrefix(string(s), "#") { //nolint:mnd // rrggbb
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		v, err := strconv.ParseUint(hex[2*i:2*i+2], 16, 8)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = int(v)
	}
	return out, true
}
