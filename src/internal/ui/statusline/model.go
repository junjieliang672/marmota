package statusline

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
)

// hotkeyLinePadding is the horizontal padding around the hotkey list.
const hotkeyLinePadding = 4

// minStatusWidth is the narrowest status line drawn.
const minStatusWidth = 40

// The status line's colors when the theme sets none: Catppuccin Mocha's
// base and text.
const (
	defaultBGColor = "#1e1e2e"
	defaultFGColor = "#cdd6f4"
)

// hotkeySeparatorWidth is the width of what goes between two hotkeys: a
// space, the separator, a space.
const hotkeySeparatorWidth = 3

// Model represents the bottom status line showing useful hotkeys
type Model struct {
	width   int
	enabled bool
	// right is pre-styled text for the right edge, such as the Claude session
	// summary or a short message after an action.
	right string
}

// New creates a new status line model
func New() Model {
	return Model{
		enabled: true,
	}
}

// SetWidth sets the width of the status line
func (m *Model) SetWidth(width int) {
	m.width = width
}

// SetRight sets the pre-styled text shown at the right edge.
func (m *Model) SetRight(text string) {
	m.right = text
}

// rightReserve is the room the hotkey list leaves for the right segment: its
// width plus a separator-sized gap.
func (m *Model) rightReserve() int {
	const gap = 3
	if m.right == "" {
		return 0
	}
	return lipgloss.Width(m.right) + gap
}

// withRightSegment pads the hotkey line to the full width and puts the right
// segment at the end when it fits.
func (m *Model) withRightSegment(line string, baseStyle lipgloss.Style) string {
	gapStyle := baseStyle.Padding(0)
	lineWidth := lipgloss.Width(line)
	rightWidth := lipgloss.Width(m.right)
	if m.right != "" && lineWidth+rightWidth+1 <= m.width {
		gap := m.width - lineWidth - rightWidth - 1
		return line + gapStyle.Render(strings.Repeat(" ", gap)) + m.right + gapStyle.Render(" ")
	}
	if lineWidth < m.width {
		line += gapStyle.Render(strings.Repeat(" ", m.width-lineWidth))
	}
	return line
}

// IsEnabled returns whether the status line is enabled
func (m *Model) IsEnabled() bool {
	return m.enabled
}

// SetEnabled sets whether the status line is enabled
func (m *Model) SetEnabled(enabled bool) {
	m.enabled = enabled
}

// Toggle toggles the status line on/off
func (m *Model) Toggle() {
	m.enabled = !m.enabled
}

// Hotkey represents a single hotkey display
type Hotkey struct {
	Key         string
	Description string
}

// Render renders the status line with hotkeys
func (m *Model) Render() string {
	if !m.enabled || m.width < 40 {
		return ""
	}

	hotkeys := defaultHotkeys()

	hotkeys = slices.DeleteFunc(hotkeys, func(h Hotkey) bool { return h.Key == "" })

	// Style for the status line
	bgColor := common.Theme.FilePanelBG
	if bgColor == "" {
		bgColor = defaultBGColor
	}
	fgColor := common.Theme.SidebarTitle
	if fgColor == "" {
		fgColor = defaultFGColor
	}
	dimFgColor := "#6c7086"

	baseStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(fgColor)).
		Padding(0, 1)

	keyStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(fgColor))

	descStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(dimFgColor))

	separatorStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(dimFgColor))

	// Build the hotkey display
	var hotkeyStrs []string
	availableWidth := m.width - hotkeyLinePadding - m.rightReserve() // Account for padding

	for _, hk := range hotkeys {
		var hkStr string
		if common.Config.Nerdfont {
			hkStr = keyStyle.Render(hk.Key) + descStyle.Render(":"+hk.Description)
		} else {
			hkStr = keyStyle.Render(hk.Key) + descStyle.Render(" "+hk.Description)
		}

		// Check if adding this hotkey would exceed available width
		currentWidth := 0
		for _, s := range hotkeyStrs {
			currentWidth += lipgloss.Width(s) + hotkeySeparatorWidth
		}

		if currentWidth+lipgloss.Width(hkStr)+hotkeySeparatorWidth > availableWidth {
			break
		}

		hotkeyStrs = append(hotkeyStrs, hkStr)
	}

	// Join with separator
	separator := separatorStyle.Render(" " + icon.Space + " ")
	content := strings.Join(hotkeyStrs, separator)

	// Add padding and border
	line := baseStyle.Render(content)

	return m.withRightSegment(line, baseStyle)
}

// RenderForContext renders context-specific hotkeys based on current state
func (m *Model) RenderForContext(context string) string {
	if !m.enabled || m.width < 40 {
		return ""
	}

	var hotkeys []Hotkey

	switch context {
	case "search":
		hotkeys = []Hotkey{
			{"Enter", "confirm"},
			{"Esc", "cancel"},
			{"Ctrl+R", "recursive"},
			{"Ctrl+F", "filter"},
		}
	case "select":
		hotkeys = []Hotkey{
			{"Space", "select"},
			{"a", "select all"},
			{"c", "copy"},
			{"x", "cut"},
			{"d", "delete"},
			{"Esc", "cancel"},
		}
	case "normal":
		hotkeys = defaultHotkeys()
	default:
		return m.Render()
	}

	return m.RenderHotkeys(hotkeys)
}

// RenderHotkeys renders the given hotkeys, as many as fit, with the right
// segment.
func (m *Model) RenderHotkeys(hotkeys []Hotkey) string {
	return m.renderHotkeys("", hotkeys)
}

// RenderModeHotkeys renders a mode's name as a badge, then the hotkeys, as
// many as fit.
func (m *Model) RenderModeHotkeys(mode string, hotkeys []Hotkey) string {
	return m.renderHotkeys(mode, hotkeys)
}

func (m *Model) renderHotkeys(mode string, hotkeys []Hotkey) string {
	if !m.enabled || m.width < minStatusWidth {
		return ""
	}
	hotkeys = slices.DeleteFunc(hotkeys, func(h Hotkey) bool { return h.Key == "" })

	// Use same rendering logic as Render()
	bgColor := common.Theme.FilePanelBG
	if bgColor == "" {
		bgColor = defaultBGColor
	}
	fgColor := common.Theme.SidebarTitle
	if fgColor == "" {
		fgColor = defaultFGColor
	}
	dimFgColor := "#6c7086"

	baseStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(fgColor)).
		Padding(0, 1)

	keyStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(fgColor))

	descStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(dimFgColor))

	separatorStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(dimFgColor))

	var hotkeyStrs []string
	availableWidth := m.width - hotkeyLinePadding - m.rightReserve()
	badge := ""
	if mode != "" {
		badge = lipgloss.NewStyle().Background(lipgloss.Color(common.Theme.FilePanelBorderActive)).
			Foreground(lipgloss.Color(bgColor)).Bold(true).Render(" "+mode+" ") +
			descStyle.Render("  ")
		availableWidth -= lipgloss.Width(badge)
	}

	for _, hk := range hotkeys {
		hkStr := keyStyle.Render(hk.Key) + descStyle.Render(":"+hk.Description)

		currentWidth := 0
		for _, s := range hotkeyStrs {
			currentWidth += lipgloss.Width(s) + hotkeySeparatorWidth
		}

		if currentWidth+lipgloss.Width(hkStr)+hotkeySeparatorWidth > availableWidth {
			break
		}

		hotkeyStrs = append(hotkeyStrs, hkStr)
	}

	separator := separatorStyle.Render(" " + icon.Space + " ")
	content := badge + strings.Join(hotkeyStrs, separator)
	line := baseStyle.Render(content)

	return m.withRightSegment(line, baseStyle)
}

// GetHeight returns the height of the status line (always 1 line)
func (m *Model) GetHeight() int {
	if m.enabled {
		return 1
	}
	return 0
}

// RenderCompact renders a compact version showing only essential hotkeys
func (m *Model) RenderCompact() string {
	if !m.enabled || m.width < 30 {
		return ""
	}

	hotkeys := []Hotkey{
		{"?", "help"},
		{">", "cmd"},
		{"s", "sidebar"},
		{"tab", "panel"},
		{"o", "order"},
		{"z", "zoxide"},
		{"/", "filter"},
		{"f", "fzf"},
		{"q", "quit"},
	}

	bgColor := common.Theme.FilePanelBG
	if bgColor == "" {
		bgColor = defaultBGColor
	}
	fgColor := common.Theme.SidebarTitle
	if fgColor == "" {
		fgColor = defaultFGColor
	}

	baseStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(fgColor)).
		Padding(0, 1)

	keyStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bgColor)).
		Foreground(lipgloss.Color(fgColor))

	var parts []string
	for _, hk := range hotkeys {
		parts = append(parts, fmt.Sprintf("%s:%s", keyStyle.Render(hk.Key), hk.Description))
	}

	content := strings.Join(parts, "  ")
	line := baseStyle.Render(content)

	return m.withRightSegment(line, baseStyle)
}

func firstKey(keys []string) string {
	for _, k := range keys {
		if k != "" {
			return k
		}
	}
	return ""
}

// defaultHotkeys are the keys the status line names, from your hotkeys:
// the Claude ones first, as in the AI-native mockup.
func defaultHotkeys() []Hotkey {
	return []Hotkey{
		{firstKey(common.Hotkeys.OpenAskBar), "ask"},
		{firstKey(common.Hotkeys.SendToAgent), "send"},
		{firstKey(common.Hotkeys.JumpToUnreadSession), "sessions"},
		{firstKey(common.Hotkeys.OpenAgentPrompt), "agent"},
		{firstKey(common.Hotkeys.SearchBar), "filter"},
		{firstKey(common.Hotkeys.OpenZoxide), "zoxide"},
		{firstKey(common.Hotkeys.OpenHelpMenu), "help"},
		{firstKey(common.Hotkeys.Quit), "quit"},
	}
}
