package rendering

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/charmbracelet/lipgloss"
)

type BorderConfig struct {
	// ANSI encoded strings are not allowed in border title and info items, for now.
	// The style is overridden with border's style.
	title string

	// titleSuffix follows the title in a dimmer color, such as "9/15".
	titleSuffix string

	// titleGlyph comes before the title in a color of its own, such as a
	// session's state.
	titleGlyph string

	// Optional info items at the bottom of the border
	infoItems []string

	// Section dividers - A slice of values within [0,height-2]
	// Signifying usage of MiddleLeft and MiddleRight borders in Left and Right borders for
	// Section divider line.
	dividerIdx []int

	// Including corners. Both should be >= 2
	width  int
	height int

	titleLeftMargin int
}

func NewBorderConfig(height int, width int) BorderConfig {
	return BorderConfig{
		height:          height,
		width:           width,
		titleLeftMargin: 1,
	}
}

func (b *BorderConfig) SetTitle(title string) {
	b.title = ansi.Strip(title)
}

// SetTitleSuffix sets text drawn after the title in its own color.
func (b *BorderConfig) SetTitleSuffix(suffix string) {
	b.titleSuffix = ansi.Strip(suffix)
}

// SetTitleGlyph sets a symbol drawn before the title.
func (b *BorderConfig) SetTitleGlyph(glyph string) {
	b.titleGlyph = ansi.Strip(glyph)
}

// titleLayout is how the top border is laid out around the title:
// TopLeft, margin Top runes, " ", [glyph, " "], title, [" ", suffix], " ",
// rest Top runes, TopRight.
type titleLayout struct {
	margin, rest         int
	glyph, title, suffix string
}

// layoutTitle fits the title and its suffix in the top border, cutting the
// suffix first and then the title. It reports false when there is no room.
func (b *BorderConfig) layoutTitle() (titleLayout, bool) {
	// excluding corners
	actualWidth := b.width - borderCornerWidth
	if b.title == "" || actualWidth < minTitleWidth {
		return titleLayout{}, false
	}
	avail := actualWidth - borderPaddingWidth // a space each side of the text
	glyph := b.titleGlyph
	if glyph != "" {
		avail -= ansi.StringWidth(glyph) + 1
	}
	title, suffix := b.title, b.titleSuffix
	if suffix != "" && ansi.StringWidth(title)+1+ansi.StringWidth(suffix) > avail {
		suffix = ""
	}
	title = ansi.Truncate(title, avail, "")
	used := ansi.StringWidth(title)
	if suffix != "" {
		used += 1 + ansi.StringWidth(suffix)
	}
	l := titleLayout{glyph: glyph, title: title, suffix: suffix, rest: avail - used}
	if l.rest > b.titleLeftMargin {
		l.margin = b.titleLeftMargin
		l.rest -= b.titleLeftMargin
	}
	return l, true
}

func (b *BorderConfig) SetInfoItems(infoItems ...string) {
	for i := range infoItems {
		infoItems[i] = ansi.Strip(infoItems[i])
	}
	b.infoItems = infoItems
}

func (b *BorderConfig) AreInfoItemsTruncated() bool {
	cnt := len(b.infoItems)
	if cnt == 0 {
		return false
	}

	actualWidth := b.width - borderCornerWidth
	// " " <content> " " border.Bottom
	availWidth := actualWidth/cnt - borderDividerWidth
	for i := range b.infoItems {
		if ansi.StringWidth(b.infoItems[i]) > availWidth {
			return true
		}
	}
	return false
}

func (b *BorderConfig) AddDivider(idx int) {
	b.dividerIdx = append(b.dividerIdx, idx)
}

// border.Top with something that takes up more than 1 runewidth will not work, so
// we only allow 1 runewidth for now, in the config. multiple things like
// border corner characters must be single rune, or else it would break rendering.
// This is all filled in one function to prevent passing around too many values
// in helper functions
func (b *BorderConfig) GetBorder(borderStrings lipgloss.Border) lipgloss.Border {
	res := borderStrings

	// excluding corners. Maybe we can move this to a utility function
	actualWidth := b.width - borderCornerWidth
	actualHeight := b.height - borderCornerWidth

	// The title sits in the top border with a space each side, as in
	// "╭─ asset 9/15 ───╮". The renderer draws it again in its own colors.
	if l, ok := b.layoutTitle(); ok {
		text := l.title
		if l.suffix != "" {
			text += " " + l.suffix
		}
		res.Top = strings.Repeat(borderStrings.Top, l.margin) + " " + text + " " +
			strings.Repeat(borderStrings.Top, l.rest)
	}

	cnt := len(b.infoItems)
	// Minimum 4 character for each info item so that at least first character is rendered
	if cnt > 0 && actualWidth >= cnt*minInfoItemWidth {
		// Max available width for each item's actual content
		// " " <content> " " border.Bottom
		availWidth := actualWidth/cnt - borderDividerWidth
		var infoText strings.Builder
		for _, item := range b.infoItems {
			infoText.WriteString(" " + ansi.Truncate(item, availWidth, "") + " " + borderStrings.Bottom)
		}

		// Fill the rest with border char.
		remainingWidth := actualWidth - ansi.StringWidth(infoText.String())

		res.Bottom = strings.Repeat(borderStrings.Bottom, remainingWidth) + infoText.String()
	}

	if len(b.dividerIdx) > 0 {
		// Update res.Left and res.Right
		leftBorder := strings.Builder{}
		rightBorder := strings.Builder{}
		di := 0
		for i := range actualHeight {
			if di < len(b.dividerIdx) && b.dividerIdx[di] == i {
				di++
				leftBorder.WriteString(borderStrings.MiddleLeft)
				rightBorder.WriteString(borderStrings.MiddleRight)
			} else {
				leftBorder.WriteString(borderStrings.Left)
				rightBorder.WriteString(borderStrings.Right)
			}
		}

		res.Left = leftBorder.String()
		res.Right = rightBorder.String()
	}

	return res
}
