package statusline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	m := New()
	assert.True(t, m.enabled, "Status line should be enabled by default")
	assert.Equal(t, 0, m.width, "Initial width should be 0")
}

func TestSetWidth(t *testing.T) {
	m := New()
	m.SetWidth(100)
	assert.Equal(t, 100, m.width, "Width should be set correctly")
}

func TestIsEnabled(t *testing.T) {
	m := New()
	assert.True(t, m.IsEnabled(), "Should be enabled by default")

	m.enabled = false
	assert.False(t, m.IsEnabled(), "Should reflect disabled state")
}

func TestSetEnabled(t *testing.T) {
	m := New()
	m.SetEnabled(false)
	assert.False(t, m.enabled, "Should be disabled after SetEnabled(false)")

	m.SetEnabled(true)
	assert.True(t, m.enabled, "Should be enabled after SetEnabled(true)")
}

func TestToggle(t *testing.T) {
	m := New()
	initialState := m.enabled

	m.Toggle()
	assert.NotEqual(t, initialState, m.enabled, "Toggle should flip the state")

	m.Toggle()
	assert.Equal(t, initialState, m.enabled, "Toggle should flip back")
}

func TestGetHeight(t *testing.T) {
	m := New()
	assert.Equal(t, 1, m.GetHeight(), "Height should be 1 when enabled")

	m.SetEnabled(false)
	assert.Equal(t, 0, m.GetHeight(), "Height should be 0 when disabled")
}

func TestRender_WhenDisabled(t *testing.T) {
	m := New()
	m.SetWidth(100)
	m.SetEnabled(false)

	output := m.Render()
	assert.Empty(t, output, "Should return empty string when disabled")
}

func TestRender_WhenWidthTooSmall(t *testing.T) {
	m := New()
	m.SetWidth(30)

	output := m.Render()
	assert.Empty(t, output, "Should return empty string when width too small")
}

func TestRender_NormalWidth(t *testing.T) {
	m := New()
	m.SetWidth(100)

	output := m.Render()
	assert.NotEmpty(t, output, "Should return non-empty string with normal width")
}

func TestRenderForContext_Search(t *testing.T) {
	m := New()
	m.SetWidth(100)

	output := m.RenderForContext("search")
	assert.NotEmpty(t, output, "Should render search context hotkeys")
}

func TestRenderForContext_Select(t *testing.T) {
	m := New()
	m.SetWidth(100)

	output := m.RenderForContext("select")
	assert.NotEmpty(t, output, "Should render select context hotkeys")
}

func TestRenderForContext_Normal(t *testing.T) {
	m := New()
	m.SetWidth(100)

	output := m.RenderForContext("normal")
	assert.NotEmpty(t, output, "Should render normal context hotkeys")
}

func TestRenderForContext_Unknown(t *testing.T) {
	m := New()
	m.SetWidth(100)

	normalOutput := m.Render()
	unknownOutput := m.RenderForContext("unknown")

	assert.Equal(t, normalOutput, unknownOutput, "Unknown context should fallback to normal render")
}

func TestRenderCompact(t *testing.T) {
	m := New()
	m.SetWidth(100)

	output := m.RenderCompact()
	assert.NotEmpty(t, output, "Should render compact version")
}

func TestRenderCompact_WhenDisabled(t *testing.T) {
	m := New()
	m.SetWidth(100)
	m.SetEnabled(false)

	output := m.RenderCompact()
	assert.Empty(t, output, "Should return empty string when disabled")
}

func TestRenderCompact_WhenWidthTooSmall(t *testing.T) {
	m := New()
	m.SetWidth(20)

	output := m.RenderCompact()
	assert.Empty(t, output, "Should return empty string when width too small")
}
